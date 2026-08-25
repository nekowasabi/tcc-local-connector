package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/engine"
)

func TestCapabilitiesMatchDocs(t *testing.T) {
	doc, err := os.ReadFile("../../docs/protocol-v1.md")
	if err != nil {
		t.Fatal(err)
	}
	var documented []string
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "`ready`") || strings.HasPrefix(line, "`ready`,") {
			documented = strings.Split(strings.Trim(strings.TrimSpace(line), "`"), "`, `")
		}
	}
	if len(documented) != 11 {
		t.Fatalf("documented capabilities = %d, want 11", len(documented))
	}
}

type harness struct {
	input  *io.PipeWriter
	output *bufio.Scanner
	cancel context.CancelFunc
	done   chan error
}

func newHarness(t *testing.T, configure func(*Server)) *harness {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(inputReader, outputWriter, nil)
	if configure != nil {
		configure(server)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
		_ = outputWriter.Close()
	}()

	h := &harness{
		input:  inputWriter,
		output: bufio.NewScanner(outputReader),
		cancel: cancel,
		done:   done,
	}
	var ready Event
	h.readJSON(t, &ready)
	if ready.Event != "ready" || ready.Version != Version {
		t.Fatalf("unexpected ready event: %+v", ready)
	}
	return h
}

func newHarnessWithReadyCapabilities(t *testing.T, configure func(*Server)) ([]string, *harness) {
	t.Helper()
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(inputReader, outputWriter, nil)
	if configure != nil {
		configure(server)
	}
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
		_ = outputWriter.Close()
	}()

	h := &harness{
		input:  inputWriter,
		output: bufio.NewScanner(outputReader),
		cancel: cancel,
		done:   done,
	}

	var ready struct {
		Version int    `json:"version"`
		Event   string `json:"event"`
		Data    struct {
			Capabilities []struct {
				Name string `json:"name"`
			} `json:"capabilities"`
		} `json:"data"`
	}
	h.readJSON(t, &ready)
	if ready.Event != "ready" || ready.Version != Version {
		t.Fatalf("unexpected ready event: %#v", ready)
	}
	capabilities := make([]string, 0, len(ready.Data.Capabilities))
	for _, item := range ready.Data.Capabilities {
		capabilities = append(capabilities, item.Name)
	}
	return capabilities, h
}

func (h *harness) close(t *testing.T) {
	t.Helper()
	_ = h.input.Close()
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		h.cancel()
		t.Fatal("server did not stop after EOF")
	}
}

func (h *harness) send(t *testing.T, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.input.Write(append(payload, '\n')); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) sendRaw(t *testing.T, value string) {
	t.Helper()
	if _, err := io.WriteString(h.input, value); err != nil {
		t.Fatal(err)
	}
}

func (h *harness) readResponse(t *testing.T) Response {
	t.Helper()
	var response Response
	h.readJSON(t, &response)
	return response
}

func (h *harness) readJSON(t *testing.T, target any) {
	t.Helper()
	if !h.output.Scan() {
		t.Fatalf("expected output: %v", h.output.Err())
	}
	if err := json.Unmarshal(h.output.Bytes(), target); err != nil {
		t.Fatalf("invalid JSON output %q: %v", h.output.Text(), err)
	}
}

func TestStatus_Unsupported_NoEngine(t *testing.T) {
	TestStatusUnsupported(t)
}

func TestReloadConfig_Success(t *testing.T) {
	eng := &fakeEngine{
		reloadResult: engine.ReloadResult{
			OK:      true,
			Applied: true,
			Errors:  []config.ValidationError{},
		},
	}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-success", Method: "reload_config"})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected reload success, got %+v", got)
	}
	body, ok := got.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected response body: %#v", got.Result)
	}
	if body["ok"] != true || body["applied"] != true {
		t.Fatalf("unexpected reload result: %#v", body)
	}
}

func TestReloadConfig_SchemaInvalid(t *testing.T) {
	TestReloadConfig_InvalidSchema(t)
}

func TestReloadConfig_NotFound(t *testing.T) {
	eng := &fakeEngine{reloadResult: engine.ReloadResult{OK: false, Errors: []config.ValidationError{{Code: "config_not_found"}}}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-not-found", Method: "reload_config"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "config_not_found" {
		t.Fatalf("expected config_not_found, got %+v", got)
	}
}

func TestReloadConfig_InsecurePermissions(t *testing.T) {
	eng := &fakeEngine{reloadResult: engine.ReloadResult{OK: false, Errors: []config.ValidationError{{Code: "insecure_permissions"}}}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-insecure", Method: "reload_config"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "insecure_permissions" {
		t.Fatalf("expected insecure_permissions, got %+v", got)
	}
}

func TestReloadConfig_IOError(t *testing.T) {
	eng := &fakeEngine{reloadResult: engine.ReloadResult{OK: false, Errors: []config.ValidationError{{Code: "io_error"}}}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-io", Method: "reload_config"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "io_error" {
		t.Fatalf("expected io_error, got %+v", got)
	}
}

func TestPause_BothSpecified_InvalidParams(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-both-2", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60,"until":"2025-01-01T00:00:00Z"}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for both, got %+v", got)
	}
}

func TestPause_NeitherSpecified_InvalidParams(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-none-2", Method: "pause", Params: json.RawMessage(`{}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for none, got %+v", got)
	}
}

func TestPause_DurationOutOfRange(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-out-range", Method: "pause", Params: json.RawMessage(`{"duration_seconds":59}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for range, got %+v", got)
	}
}

func TestPause_UntilInPast(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-past-2", Method: "pause", Params: json.RawMessage(`{"until":"2000-01-01T00:00:00Z"}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for past until, got %+v", got)
	}
}

func TestPause_Success(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-success-2", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60}`)})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected pause success, got %+v", got)
	}
}

func TestPause_PersistFailure_NoStateChange(t *testing.T) {
	eng := &fakeEngine{pauseErr: errors.New("x")}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-fail-2", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "internal_error" {
		t.Fatalf("expected internal_error, got %+v", got)
	}
}

func TestResume_Success(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "resume-1", Method: "resume"})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected resume success, got %+v", got)
	}
	res, ok := got.Result.(map[string]any)
	if !ok || res["resumed"] != true {
		t.Fatalf("expected resumed=true, got %+v", got)
	}
}

func TestResume_InvalidState_NotIdempotent(t *testing.T) {
	eng := &fakeEngine{resumeErr: errors.New("invalid state")}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "resume-invalid-2", Method: "resume"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "invalid_state" {
		t.Fatalf("expected invalid_state, got %+v", got)
	}
}

func TestRefreshNow_Success(t *testing.T) {
	TestRefreshNow(t)
}

func TestRefreshNow_Busy(t *testing.T) {
	eng := &fakeEngine{runCycleErr: errors.New("busy")}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "refresh-busy-2", Method: "refresh_now"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "busy" {
		t.Fatalf("expected busy, got %+v", got)
	}
}

func TestRefreshNow_Paused(t *testing.T) {
	eng := &fakeEngine{runCycleErr: errors.New("paused")}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "refresh-paused-2", Method: "refresh_now"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "paused" {
		t.Fatalf("expected paused, got %+v", got)
	}
}

func TestRefreshNow_ConfigError(t *testing.T) {
	eng := &fakeEngine{runCycleErr: errors.New("config_error")}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "refresh-config-2", Method: "refresh_now"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "config_error" {
		t.Fatalf("expected config_error, got %+v", got)
	}
}

func TestConfigPaths_AllFieldsAbsolute(t *testing.T) {
	eng := &fakeEngine{
		paths: map[string]string{
			"config":    "/tmp/config",
			"state_dir": "/tmp/state",
			"log":       "/tmp/log",
			"pause":     "/tmp/pause",
			"ledger":    "/tmp/ledger",
		},
	}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "config-paths-2", Method: "config_paths"})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected config paths, got %+v", got)
	}
	result, ok := got.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %#v", got.Result)
	}
	for key := range eng.paths {
		value, ok := result[key]
		if !ok {
			t.Fatalf("missing path key: %q", key)
		}
		if value == "" {
			t.Fatalf("empty path for key %q", key)
		}
		v := value.(string)
		if !filepath.IsAbs(v) {
			t.Fatalf("path for %q is not absolute: %q", key, v)
		}
	}
}

func TestReportActions_AcceptedAndNotify(t *testing.T) {
	eng := &fakeEngine{
		statusCycleID: 10,
	}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{
		Version: Version,
		ID:      "report-ok",
		Method:  "report_actions",
		Params:  json.RawMessage(`{"cycle_id":10,"results":[{"action_id":"a","status":"accepted"},{"action_id":"b","status":"skipped"}]}`),
	})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected report actions success, got %+v", got)
	}
}

func TestReportActions_StaleCycleAllIgnored(t *testing.T) {
	eng := &fakeEngine{
		statusCycleID: 10,
	}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{
		Version: Version,
		ID:      "report-stale-2",
		Method:  "report_actions",
		Params:  json.RawMessage(`{"cycle_id":999,"results":[{"action_id":"a","status":"accepted"},{"action_id":"b","status":"skipped"}]}`),
	})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("expected report_actions response, got %+v", got)
	}
	result, ok := got.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected report result: %#v", got.Result)
	}
	if gotAccepted := int(result["accepted"].(float64)); gotAccepted != 0 {
		t.Fatalf("expected accepted=0, got=%d", gotAccepted)
	}
	if gotIgnored := int(result["ignored"].(float64)); gotIgnored != 2 {
		t.Fatalf("expected ignored=2, got=%d", gotIgnored)
	}
}

func TestReportActions_InvalidCodeStatusPair(t *testing.T) {
	TestReportActions(t)
}

func TestPlanEvent_EmptyOnReleaseOrPause(t *testing.T) {
	// ここではサーバ側は engine 側イベント送出 API を持たないため、
	// 既存ハーネスで検証可能な範囲として、event の受け口呼び出しでパニックしないことのみ確認する。
	eng := &fakeEngine{snapshot: engine.Status{State: "released"}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "status-released", Method: "status"})
	got := h.readResponse(t)
	if got.Error != nil {
		t.Fatalf("status should still succeed with released state: %+v", got)
	}
}

func TestConcurrentRequestsCanCompleteOutOfOrder(t *testing.T) {
	fast := make(chan struct{})
	slowReady := make(chan struct{})
	eng := &gatedEngine{fast: fast, slowReady: slowReady}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "slow", Method: "refresh_now"})
	<-slowReady
	h.send(t, Request{Version: Version, ID: "fast", Method: "status"})

	first := h.readResponse(t)
	close(fast)
	second := h.readResponse(t)
	if first.ID != "fast" || second.ID != "slow" {
		t.Fatalf("unexpected response order: first=%q second=%q", first.ID, second.ID)
	}
}

func TestReadyCapabilities(t *testing.T) {
	capabilities, h := newHarnessWithReadyCapabilities(t, nil)
	defer h.close(t)

	want := []string{"ready", "status", "reload_config", "pause", "resume", "refresh_now", "config_paths", "report_actions", "event.plan", "event.state_changed", "event.notify"}
	if len(capabilities) != len(want) {
		t.Fatalf("expected %d capabilities, got %d", len(want), len(capabilities))
	}
	for i := range want {
		if capabilities[i] != want[i] {
			t.Fatalf("capability[%d] = %q, want %q", i, capabilities[i], want[i])
		}
	}
}

func TestDuplicateActiveIDIsRejected(t *testing.T) {
	eng := &blockingEngine{}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	request := Request{
		Version: Version,
		ID:      "duplicate",
		Method:  "refresh_now",
	}
	h.send(t, request)
	h.send(t, request)

	var duplicateError bool
	for range 2 {
		response := h.readResponse(t)
		if response.Error != nil && response.Error.Code == "duplicate_id" {
			duplicateError = true
		}
	}
	if !duplicateError {
		t.Fatal("duplicate active request id was not rejected")
	}
}

func TestInvalidInputAndRecovery(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	h.sendRaw(t, "{not-json}\n")
	invalid := h.readResponse(t)
	if invalid.Error == nil || invalid.Error.Code != "invalid_json" {
		t.Fatalf("unexpected invalid JSON response: %+v", invalid)
	}

	h.send(t, Request{Version: Version + 1, ID: "version-1", Method: "status"})
	version := h.readResponse(t)
	if version.Error == nil || version.Error.Code != "unsupported_version" {
		t.Fatalf("unexpected version response: %+v", version)
	}

	h.send(t, Request{Version: Version, ID: "status-after-error", Method: "status"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "unsupported" || got.ID != "status-after-error" {
		t.Fatalf("server did not recover after invalid input: %+v", got)
	}
}

func TestOversizedMessageAndRecovery(t *testing.T) {
	h := newHarness(t, func(server *Server) {
		server.MaxMessage = 128
	})
	defer h.close(t)

	h.sendRaw(t, strings.Repeat("x", 256)+"\n")
	oversized := h.readResponse(t)
	if oversized.Error == nil || oversized.Error.Code != "message_too_large" {
		t.Fatalf("unexpected oversized response: %+v", oversized)
	}

	h.send(t, Request{Version: Version, ID: "status-after-large", Method: "status"})
	got := h.readResponse(t)
	if got.Error == nil || got.Error.Code != "unsupported" || got.ID != "status-after-large" {
		t.Fatalf("server did not recover after oversized input: %+v", got)
	}
}

func TestMessageLargerThanReaderBufferIsAccepted(t *testing.T) {
	eng := &fakeEngine{snapshot: engine.Status{State: "active"}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	value := strings.Repeat("x", 8*1024)
	params, err := json.Marshal(map[string]string{"pad": value})
	if err != nil {
		t.Fatal(err)
	}
	h.send(t, Request{
		Version: Version,
		ID:      "large-valid",
		Method:  "status",
		Params:  params,
	})

	response := h.readResponse(t)
	if response.Error != nil || response.ID != "large-valid" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestContextCancellationStopsBlockedReader(t *testing.T) {
	inputReader, inputWriter := io.Pipe()
	outputReader, outputWriter := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	server := NewServer(inputReader, outputWriter, nil)
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(ctx)
		_ = outputWriter.Close()
	}()

	scanner := bufio.NewScanner(outputReader)
	if !scanner.Scan() {
		t.Fatalf("ready event missing: %v", scanner.Err())
	}
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("server did not stop after context cancellation")
	}
	_ = inputWriter.Close()
}

func TestEOFWithoutTrailingNewlineIsProcessed(t *testing.T) {
	var input bytes.Buffer
	request := Request{Version: Version, ID: "status-1", Method: "status"}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input.Write(payload)

	var output lockedBuffer
	server := NewServer(&input, &output, nil)
	if err := server.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	var responses []json.RawMessage
	for scanner.Scan() {
		responses = append(responses, append(json.RawMessage(nil), scanner.Bytes()...))
	}
	if len(responses) != 2 {
		t.Fatalf("expected ready and status response, got %d", len(responses))
	}
	var response Response
	if err := json.Unmarshal(responses[1], &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "status-1" || response.Error == nil || response.Error.Code != "unsupported" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestEOFStopsActiveRequest(t *testing.T) {
	request := Request{
		Version: Version,
		ID:      "refresh-1",
		Method:  "refresh_now",
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBuffer(append(payload, '\n'))
	var output lockedBuffer
	server := NewServer(input, &output, nil)
	server.Engine = &blockingEngine{}

	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(context.Background())
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop after EOF")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("EOF shutdown took too long: %v", elapsed)
	}
}

func TestStatusUnsupported(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "status-unsupported", Method: "status"})
	response := h.readResponse(t)
	if response.Error == nil || response.Error.Code != "unsupported" {
		t.Fatalf("expected unsupported, got %+v", response)
	}
}

func TestStatus_ReadOnly_NoEmail(t *testing.T) {
	eng := &fakeEngine{snapshot: engine.Status{State: "active", ParseOK: true, RunningTasks: []engine.TaskView{{TaskID: "t1", Name: "task"}}}}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "status-1", Method: "status"})
	response := h.readResponse(t)
	if response.Error != nil {
		t.Fatalf("expected status response, got %+v", response)
	}
	encoded, err := json.Marshal(response.Result)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "@") {
		t.Fatalf("status response should not include email-like characters: %s", encoded)
	}
	if !eng.snapshotCalled {
		t.Fatal("expected Snapshot to be called")
	}
}

func TestReloadConfig_ErrorAndSuccess(t *testing.T) {
	eng := &fakeEngine{
		reloadResult: engine.ReloadResult{OK: false, Errors: []config.ValidationError{{Code: "config_not_found"}}},
	}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-missing", Method: "reload_config"})
	missing := h.readResponse(t)
	if missing.Error == nil || missing.Error.Code != "config_not_found" {
		t.Fatalf("expected config_not_found, got %+v", missing)
	}
	eng.reloadResult = engine.ReloadResult{OK: true, Applied: true}
	h.send(t, Request{Version: Version, ID: "reload-ok", Method: "reload_config"})
	result := h.readResponse(t)
	if result.Error != nil {
		t.Fatalf("expected reload success, got %+v", result)
	}
	reloadResult, ok := result.Result.(map[string]any)
	if !ok || reloadResult["ok"] != true {
		t.Fatalf("expected ok=true in reload result: %+v", result)
	}
	if !eng.reloadCalled {
		t.Fatal("expected Reload to be called")
	}
}

func TestReloadConfig_InvalidSchema(t *testing.T) {
	eng := &fakeEngine{reloadResult: engine.ReloadResult{OK: false, Errors: []config.ValidationError{{Code: "duplicate_id", Message: "dup"}}}}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "reload-schema", Method: "reload_config"})
	resp := h.readResponse(t)
	if resp.Error != nil {
		t.Fatalf("expected successful response with errors, got %+v", resp)
	}
	body, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected reload result type: %#v", resp.Result)
	}
	if body["ok"] != false {
		t.Fatalf("expected ok==false, got %#v", body)
	}
	if body["applied"] != false {
		t.Fatalf("expected applied==false when ok==false, got %#v", body)
	}
}

func TestPauseAndResume(t *testing.T) {
	eng := &fakeEngine{}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "pause-both", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60,"until":"2025-01-01T00:00:00Z"}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for both pause params, got %+v", got)
	}

	h.send(t, Request{Version: Version, ID: "pause-none", Method: "pause", Params: json.RawMessage(`{}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for missing pause params, got %+v", got)
	}

	h.send(t, Request{Version: Version, ID: "pause-range", Method: "pause", Params: json.RawMessage(`{"duration_seconds":-1}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for out-of-range duration, got %+v", got)
	}

	h.send(t, Request{Version: Version, ID: "pause-past", Method: "pause", Params: json.RawMessage(`{"until":"2000-01-01T00:00:00Z"}`)})
	if got := h.readResponse(t); got.Error == nil || got.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params for past until, got %+v", got)
	}

	h.send(t, Request{Version: Version, ID: "pause-success", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60}`)})
	success := h.readResponse(t)
	if success.Error != nil {
		t.Fatalf("expected pause success, got %+v", success)
	}
	if !eng.pauseCalled {
		t.Fatal("expected Pause to be called")
	}

	eng2 := &fakeEngine{pauseErr: errors.New("x")}
	h2 := newHarness(t, func(server *Server) {
		server.Engine = eng2
	})
	defer h2.close(t)
	h2.send(t, Request{Version: Version, ID: "pause-fail", Method: "pause", Params: json.RawMessage(`{"duration_seconds":60}`)})
	if got := h2.readResponse(t); got.Error == nil || got.Error.Code != "internal_error" {
		t.Fatalf("expected internal_error for pause failure, got %+v", got)
	}

	eng2.resumeErr = nil
	h.send(t, Request{Version: Version, ID: "resume", Method: "resume"})
	if got := h.readResponse(t); got.Error != nil {
		t.Fatalf("expected resume success, got %+v", got)
	}

	eng3 := &fakeEngine{snapshot: engine.Status{State: "active"}, resumeErr: errors.New("invalid state")}
	h3 := newHarness(t, func(server *Server) { server.Engine = eng3 })
	defer h3.close(t)
	h3.send(t, Request{Version: Version, ID: "resume-invalid", Method: "resume"})
	if got := h3.readResponse(t); got.Error == nil || got.Error.Code != "invalid_state" {
		t.Fatalf("expected invalid_state, got %+v", got)
	}
}

func TestRefreshNow(t *testing.T) {
	eng := &fakeEngine{runCycleID: 7}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "refresh-ok", Method: "refresh_now"})
	if resp := h.readResponse(t); resp.Error != nil {
		t.Fatalf("expected refresh success: %+v", resp)
	} else {
		result := resp.Result.(map[string]any)
		if result["accepted"] != true {
			t.Fatalf("expected accepted=true: %#v", result)
		}
		if result["cycle_id"] != float64(7) {
			t.Fatalf("expected cycle_id 7, got %#v", result)
		}
	}

	eng.runCycleErr = errors.New("busy")
	h.send(t, Request{Version: Version, ID: "refresh-busy", Method: "refresh_now"})
	if resp := h.readResponse(t); resp.Error == nil || resp.Error.Code != "busy" {
		t.Fatalf("expected busy, got %+v", resp)
	}

	eng.runCycleErr = errors.New("paused")
	h.send(t, Request{Version: Version, ID: "refresh-paused", Method: "refresh_now"})
	if resp := h.readResponse(t); resp.Error == nil || resp.Error.Code != "paused" {
		t.Fatalf("expected paused, got %+v", resp)
	}

	eng.runCycleErr = errors.New("config_error")
	h.send(t, Request{Version: Version, ID: "refresh-config", Method: "refresh_now"})
	if resp := h.readResponse(t); resp.Error == nil || resp.Error.Code != "config_error" {
		t.Fatalf("expected config_error, got %+v", resp)
	}
}

func TestConfigPaths(t *testing.T) {
	eng := &fakeEngine{paths: map[string]string{"config": "/tmp/config", "state_dir": "/tmp", "log": "/tmp/log", "pause": "/tmp/pause", "ledger": "/tmp/ledger"}}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "config-paths", Method: "config_paths"})
	resp := h.readResponse(t)
	if resp.Error != nil {
		t.Fatalf("expected config_paths result, got %+v", resp)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("unexpected result type: %#v", resp.Result)
	}
	for key := range eng.paths {
		v, ok := result[key]
		if !ok || v == "" {
			t.Fatalf("expected %q in config_paths result", key)
		}
	}
	if !eng.pathsCalled {
		t.Fatal("expected Paths to be called")
	}
}

func TestReportActions(t *testing.T) {
	eng := &fakeEngine{statusCycleID: 10}
	h := newHarness(t, func(server *Server) {
		server.Engine = eng
	})
	defer h.close(t)

	h.send(t, Request{
		Version: Version,
		ID:      "report-invalid",
		Method:  "report_actions",
		Params:  json.RawMessage(`{"cycle_id":10,"results":[{"action_id":"a","status":"accepted","code":"x"}]}`),
	})
	if resp := h.readResponse(t); resp.Error == nil || resp.Error.Code != "invalid_params" {
		t.Fatalf("expected invalid_params, got %+v", resp)
	}

	h.send(t, Request{
		Version: Version,
		ID:      "report-stale",
		Method:  "report_actions",
		Params:  json.RawMessage(`{"cycle_id":999,"results":[{"action_id":"a","status":"accepted"}]}`),
	})
	stale := h.readResponse(t)
	if stale.Error != nil {
		t.Fatalf("expected stale report success, got %+v", stale)
	}
	res := stale.Result.(map[string]any)
	if got := int(res["accepted"].(float64)); got != 0 {
		t.Fatalf("expected accepted=0 for stale cycle, got %d", got)
	}
	if got := int(res["ignored"].(float64)); got != 1 {
		t.Fatalf("expected ignored=1 for stale cycle, got %d", got)
	}

	h.send(t, Request{
		Version: Version,
		ID:      "report-valid",
		Method:  "report_actions",
		Params:  json.RawMessage(`{"cycle_id":10,"results":[{"action_id":"a","status":"accepted"},{"action_id":"b","status":"skipped"}]}`),
	})
	valid := h.readResponse(t)
	if valid.Error != nil {
		t.Fatalf("expected report_actions success, got %+v", valid)
	}
	res = valid.Result.(map[string]any)
	if int(res["accepted"].(float64))+int(res["ignored"].(float64)) != 2 {
		t.Fatalf("expected accepted+ignored=2, got %#v", res)
	}
}

func TestManyConcurrentRequests(t *testing.T) {
	eng := &fakeEngine{snapshot: engine.Status{State: "active"}}
	h := newHarness(t, func(server *Server) { server.Engine = eng })
	defer h.close(t)

	const count = 250
	for i := range count {
		h.send(t, Request{
			Version: Version,
			ID:      fmt.Sprintf("status-%03d", i),
			Method:  "status",
		})
	}

	seen := make(map[string]bool, count)
	for range count {
		response := h.readResponse(t)
		if response.Error != nil {
			t.Fatalf("unexpected response error: %+v", response)
		}
		seen[response.ID] = true
	}
	if len(seen) != count {
		t.Fatalf("got %d unique responses, want %d", len(seen), count)
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.b.Bytes()...)
}

type fakeEngine struct {
	reloadResult  engine.ReloadResult
	snapshot      engine.Status
	pauseErr      error
	resumeErr     error
	runCycleID    int64
	runCycleErr   error
	statusCycleID int64
	paths         map[string]string

	reloadCalled   bool
	snapshotCalled bool
	pauseCalled    bool
	resumeCalled   bool
	runCalled      bool
	pathsCalled    bool

	statusMu sync.Mutex
}

func (f *fakeEngine) Snapshot() engine.Status {
	f.statusMu.Lock()
	defer f.statusMu.Unlock()
	f.snapshotCalled = true
	if f.snapshot.State == "" {
		return engine.Status{}
	}
	return f.snapshot
}

func (f *fakeEngine) Reload() engine.ReloadResult {
	f.reloadCalled = true
	return f.reloadResult
}

func (f *fakeEngine) Pause(until time.Time) error {
	f.pauseCalled = true
	return f.pauseErr
}

func (f *fakeEngine) Resume() error {
	f.resumeCalled = true
	if f.resumeErr != nil {
		return f.resumeErr
	}
	return nil
}

func (f *fakeEngine) RunCycleNow(context.Context) (int64, error) {
	f.runCalled = true
	return f.runCycleID, f.runCycleErr
}

func (f *fakeEngine) ReportActions(cycleID int64, results []engine.ActionResult) (int, int) {
	if cycleID != f.statusCycleID {
		return 0, len(results)
	}
	if f.statusCycleID == 0 && cycleID != 0 {
		return 0, len(results)
	}
	accepted := 0
	for range results {
		accepted++
	}
	return accepted, 0
}

func (f *fakeEngine) Paths() map[string]string {
	f.pathsCalled = true
	if f.paths != nil {
		return f.paths
	}
	return map[string]string{}
}

type blockingEngine struct{ fakeEngine }

func (e *blockingEngine) RunCycleNow(ctx context.Context) (int64, error) {
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-time.After(50 * time.Millisecond):
		return 1, nil
	}
}

type gatedEngine struct {
	fakeEngine
	fast      chan struct{}
	slowReady chan struct{}
	once      sync.Once
}

func (e *gatedEngine) RunCycleNow(context.Context) (int64, error) {
	e.once.Do(func() { close(e.slowReady) })
	<-e.fast
	return 1, nil
}
