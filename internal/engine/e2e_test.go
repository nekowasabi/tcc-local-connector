package engine

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestEndToEnd covers SC-01 ready, SC-03 plan/report_actions, and SC-07 pause/resume.
func TestEndToEnd(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", ".."))
	tempDir := t.TempDir()
	binary := filepath.Join(tempDir, "tcc-local-connector-backend")
	build := exec.Command("go", "build", "-o", binary, "./cmd/tcc-local-connector-backend")
	build.Dir = repoRoot
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build backend: %v\n%s", err, output)
	}
	fake, err := filepath.Abs(filepath.Join("testdata", "fake-tcc2.sh"))
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(tempDir, "config.yml")
	config := "version: 2\n" +
		"task_source:\n  executable: " + fake + "\n" +
		"polling:\n  interval_seconds: 10\n  timeout_seconds: 1\n  failure_grace_seconds: 0\n  failure_policy: release_controls\n" +
		"rules:\n  - id: e2e\n    ensure:\n      - type: notify\n        title: e2e\n        message: notification\n"
	if err := os.WriteFile(configPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(binary, "serve", "--stdio", "--config", configPath)
	var stderr e2eLog
	command.Stderr = &stderr
	stdin, err := command.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = command.Wait()
	})

	reader := bufio.NewReader(stdout)
	ready := readEnvelope(t, reader, &stderr)
	if ready.Event != "ready" {
		t.Fatalf("first message = %#v, want ready", ready)
	}
	t.Log("ready")
	writeRequest(t, stdin, "reload", "reload_config", map[string]any{})
	_ = readResponse(t, reader, &stderr, "reload")

	writeRequest(t, stdin, "refresh", "refresh_now", map[string]any{})
	var cycleID int64
	seenRefresh, seenPlan := false, false
	for !seenRefresh || !seenPlan {
		message := readEnvelope(t, reader, &stderr)
		if message.Event == "event.plan" {
			var plan struct {
				CycleID int64 `json:"cycle_id"`
			}
			if err := json.Unmarshal(message.Data, &plan); err != nil {
				t.Fatal(err)
			}
			cycleID = plan.CycleID
			seenPlan = true
			t.Log("plan")
		}
		if message.ID == "refresh" {
			seenRefresh = true
		}
	}
	if cycleID == 0 {
		t.Fatal("plan did not contain a cycle_id")
	}

	writeRequest(t, stdin, "report", "report_actions", map[string]any{"cycle_id": cycleID, "results": []map[string]string{{"action_id": "1-1", "status": "accepted"}}})
	report := readResponse(t, reader, &stderr, "report")
	var reported struct {
		Accepted int `json:"accepted"`
	}
	if err := json.Unmarshal(report.Result, &reported); err != nil || reported.Accepted != 1 {
		t.Fatalf("report_actions = %#v, %v", report, err)
	}
	t.Log("report_actions accepted")

	writeRequest(t, stdin, "pause", "pause", map[string]any{"duration_seconds": int(time.Minute / time.Second)})
	_ = readResponse(t, reader, &stderr, "pause")
	t.Log("paused")
	writeRequest(t, stdin, "resume", "resume", map[string]any{})
	_ = readResponse(t, reader, &stderr, "resume")
	t.Log("resumed")
}

type e2eEnvelope struct {
	ID     string          `json:"id"`
	Event  string          `json:"event"`
	Data   json.RawMessage `json:"data"`
	Result json.RawMessage `json:"result"`
}

func writeRequest(t *testing.T, writer io.Writer, id, method string, params any) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"version": 1, "id": id, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func readResponse(t *testing.T, reader *bufio.Reader, stderr *e2eLog, id string) e2eEnvelope {
	t.Helper()
	for {
		message := readEnvelope(t, reader, stderr)
		if message.ID == id {
			return message
		}
	}
}

func readEnvelope(t *testing.T, reader *bufio.Reader, stderr *e2eLog) e2eEnvelope {
	t.Helper()
	type result struct {
		line []byte
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		line, err := reader.ReadBytes('\n')
		resultCh <- result{line: line, err: err}
	}()
	var read result
	select {
	case read = <-resultCh:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for backend output; stderr=%s", stderr.String())
	}
	if read.err != nil {
		t.Fatalf("read backend output: %v; stderr=%s", read.err, stderr.String())
	}
	var message e2eEnvelope
	if err := json.Unmarshal(read.line, &message); err != nil {
		t.Fatalf("decode backend message %q: %v", read.line, err)
	}
	return message
}

type e2eLog struct {
	mu sync.Mutex
	bytes.Buffer
}

func (l *e2eLog) Write(value []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Buffer.Write(value)
}

func (l *e2eLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.Buffer.String()
}
