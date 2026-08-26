package frontend

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
)

var errNotLaunched = errors.New("backend is not launched")

type Client struct {
	mu            sync.Mutex
	state         State
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	framer        LineFramer
	plans         []PlanPayload
	notifications []NotifyPayload
	responses     []Response
	nextID        int
}

func NewClient() *Client {
	return &Client{state: StateTerminated}
}

func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Client) Launch(executable string, args []string) error {
	c.Shutdown()
	cmd := exec.Command(executable, args...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return err
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		return err
	}
	c.mu.Lock()
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.state = StateStarting
	c.framer = LineFramer{}
	c.mu.Unlock()
	go c.readStdout(stdout)
	go c.drain(stderr)
	return nil
}

func (c *Client) Send(method string, params any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return errNotLaunched
	}
	c.nextID++
	request := Request{Version: SupportedProtocolVersion, ID: fmt.Sprintf("menu-%d", c.nextID), Method: method, Params: params}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	_, err = c.stdin.Write(payload)
	return err
}

func (c *Client) TakePlans() []PlanPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	plans := c.plans
	c.plans = nil
	return plans
}

func (c *Client) TakeResponses() []Response {
	c.mu.Lock()
	defer c.mu.Unlock()
	responses := c.responses
	c.responses = nil
	return responses
}

func (c *Client) TakeNotifications() []NotifyPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	notifications := c.notifications
	c.notifications = nil
	return notifications
}

func (c *Client) ConsumeStdout(data []byte) {
	c.mu.Lock()
	lines := c.framer.Push(data)
	c.mu.Unlock()
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event Event
		if json.Unmarshal(line, &event) == nil && event.Event != "" && event.IDMissing() {
			c.handleEvent(event)
			continue
		}
		var response Response
		if json.Unmarshal(line, &response) == nil && response.ID != "" {
			c.mu.Lock()
			c.responses = append(c.responses, response)
			c.mu.Unlock()
		}
	}
}

func (event Event) IDMissing() bool {
	return event.Event != ""
}

func (c *Client) AcceptReady(protocolVersion int, capabilities []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if protocolVersion != SupportedProtocolVersion || !hasAll(capabilities, RequiredCapabilities) {
		c.state = StateBackendIncompatible
		return
	}
	c.state = StateRunning
}

func (c *Client) Shutdown() {
	c.mu.Lock()
	stdin := c.stdin
	stdout := c.stdout
	stderr := c.stderr
	cmd := c.cmd
	c.stdin = nil
	c.stdout = nil
	c.stderr = nil
	c.cmd = nil
	c.state = StateTerminated
	c.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if stdout != nil {
		_ = stdout.Close()
	}
	if stderr != nil {
		_ = stderr.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
}

func (c *Client) handleEvent(event Event) {
	switch event.Event {
	case "ready":
		version := intFrom(event.Data["protocol_version"])
		capabilities := stringNames(event.Data["capabilities"])
		c.AcceptReady(version, capabilities)
	case "event.plan":
		payload, _ := json.Marshal(event.Data)
		var plan PlanPayload
		if json.Unmarshal(payload, &plan) == nil {
			c.mu.Lock()
			c.plans = append(c.plans, plan)
			c.mu.Unlock()
		}
	case "event.notify":
		payload, _ := json.Marshal(event.Data)
		var notification NotifyPayload
		if json.Unmarshal(payload, &notification) == nil {
			c.mu.Lock()
			c.notifications = append(c.notifications, notification)
			c.mu.Unlock()
		}
	}
}

func (c *Client) readStdout(r io.Reader) {
	reader := bufio.NewReader(r)
	buf := make([]byte, 4096)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			c.ConsumeStdout(append([]byte(nil), buf[:n]...))
		}
		if err != nil {
			return
		}
	}
}

func (c *Client) drain(r io.Reader) {
	_, _ = io.Copy(io.Discard, r)
}

func hasAll(have, need []string) bool {
	set := map[string]bool{}
	for _, value := range have {
		set[value] = true
	}
	for _, value := range need {
		if !set[value] {
			return false
		}
	}
	return true
}

func stringNames(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		switch typed := item.(type) {
		case string:
			names = append(names, typed)
		case map[string]any:
			if name, ok := typed["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

func intFrom(value any) int {
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}
