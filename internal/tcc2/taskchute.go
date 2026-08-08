package tcc2

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type ToolResult struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}
type Session struct {
	cmd    *exec.Cmd
	input  io.WriteCloser
	output *bufio.Reader
	nextID int
	mu     sync.Mutex
}

func Open(ctx context.Context, executable string, args []string) (*Session, error) {
	if len(args) == 0 {
		args = []string{"mcp"}
	}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.WaitDelay = constants.MCPWaitDelaySeconds * time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	s := &Session{cmd: cmd, input: in, output: bufio.NewReader(out)}
	if _, err := s.request(ctx, "initialize", map[string]any{"protocolVersion": mcpProtocolVersion, "capabilities": map[string]any{}, "clientInfo": map[string]string{"name": "tcc-local-connector", "version": "1"}}); err != nil {
		_ = s.Close()
		return nil, err
	}
	if err := json.NewEncoder(in).Encode(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized", "params": map[string]any{}}); err != nil {
		_ = s.Close()
		return nil, err
	}
	return s, nil
}
func (s *Session) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	id := s.nextID
	if err := json.NewEncoder(s.input).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	return s.read(ctx, id)
}
func (s *Session) read(ctx context.Context, expected int) (json.RawMessage, error) {
	type reply struct {
		payload []byte
		err     error
	}
	for {
		done := make(chan reply, 1)
		go func() { line, err := readBoundedLine(s.output, maxResponseBytes); done <- reply{line, err} }()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-done:
			if r.err != nil {
				return nil, r.err
			}
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(r.payload, &response); err != nil {
				return nil, err
			}
			if response.ID != expected {
				continue
			}
			if len(response.Error) > 0 && string(response.Error) != "null" {
				return nil, fmt.Errorf("MCP response error")
			}
			return response.Result, nil
		}
	}
}
func (s *Session) Call(ctx context.Context, name string, arguments map[string]any) (ToolResult, error) {
	raw, err := s.request(ctx, "tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return ToolResult{}, err
	}
	var result ToolResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return ToolResult{}, err
	}
	return result, nil
}
func (s *Session) Close() error {
	if s == nil || s.cmd == nil {
		return nil
	}
	_ = s.input.Close()
	return s.cmd.Wait()
}

type cachedUser struct {
	info UserInfo
	at   time.Time
}

var userCache struct {
	sync.Mutex
	value cachedUser
}

func InvalidateUserCache() {
	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()
}

func FetchUser(ctx context.Context, session *Session) (UserInfo, error) {
	result, err := session.Call(ctx, "get_user", map[string]any{})
	if err != nil {
		return UserInfo{}, err
	}
	return parseUserText(joinText(result)), nil
}
func FetchTaskChute(ctx context.Context, executable string, args []string, viewID *string) (TaskChuteResult, error) {
	s, err := Open(ctx, executable, args)
	if err != nil {
		return TaskChuteResult{}, err
	}
	defer s.Close()
	userCache.Lock()
	cached := userCache.value
	userCache.Unlock()
	info := cached.info
	fallback := false
	if cached.at.IsZero() || time.Since(cached.at) > constants.UserInfoTTLSeconds*time.Second {
		info, err = FetchUser(ctx, s)
		if err != nil {
			fallback = true
			info = UserInfo{}
		} else {
			userCache.Lock()
			userCache.value = cachedUser{info, time.Now()}
			userCache.Unlock()
		}
	}
	location := time.Local
	if info.Timezone != "" {
		if loaded, loadErr := time.LoadLocation(info.Timezone); loadErr == nil {
			location = loaded
		}
	}
	now := time.Now().In(location)
	end := now.Format("2006-01-02")
	start := now.AddDate(0, 0, -1).Format("2006-01-02")
	arguments := map[string]any{"start_date": start, "end_date": end}
	if viewID != nil {
		arguments["view_id"] = *viewID
	}
	result, err := s.Call(ctx, "get_taskchute", arguments)
	if err != nil {
		return TaskChuteResult{}, err
	}
	parsed, err := ParseTaskChuteText(joinText(result), result.IsError)
	if fallback {
		parsed.Warnings = append(parsed.Warnings, "user_info_fallback")
	}
	return parsed, err
}
func joinText(result ToolResult) string {
	var text string
	for _, item := range result.Content {
		if item.Type == "text" {
			text += item.Text
		}
	}
	return text
}
