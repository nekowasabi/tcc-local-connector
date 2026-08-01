package protocol

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/tcc2"
)

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

func TestHealthAndEcho(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "health-1", Method: "health"})
	health := h.readResponse(t)
	if health.Error != nil || health.ID != "health-1" {
		t.Fatalf("unexpected health response: %+v", health)
	}

	h.send(t, Request{
		Version: Version,
		ID:      "echo-1",
		Method:  "echo",
		Params:  json.RawMessage(`{"value":{"text":"こんにちは","number":42}}`),
	})
	echo := h.readResponse(t)
	if echo.Error != nil || echo.ID != "echo-1" {
		t.Fatalf("unexpected echo response: %+v", echo)
	}
}

func TestConcurrentRequestsCanCompleteOutOfOrder(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	h.send(t, Request{
		Version: Version,
		ID:      "slow",
		Method:  "sleep",
		Params:  json.RawMessage(`{"milliseconds":100}`),
	})
	h.send(t, Request{
		Version: Version,
		ID:      "fast",
		Method:  "sleep",
		Params:  json.RawMessage(`{"milliseconds":1}`),
	})

	first := h.readResponse(t)
	second := h.readResponse(t)
	if first.ID != "fast" || second.ID != "slow" {
		t.Fatalf("unexpected response order: first=%q second=%q", first.ID, second.ID)
	}
}

func TestCancellation(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	h.send(t, Request{
		Version: Version,
		ID:      "sleep-1",
		Method:  "sleep",
		Params:  json.RawMessage(`{"milliseconds":5000}`),
	})
	h.send(t, Request{
		Version: Version,
		ID:      "cancel-1",
		Method:  "cancel",
		Params:  json.RawMessage(`{"id":"sleep-1"}`),
	})

	responses := map[string]Response{}
	for range 2 {
		response := h.readResponse(t)
		responses[response.ID] = response
	}
	if responses["cancel-1"].Error != nil {
		t.Fatalf("cancel request failed: %+v", responses["cancel-1"])
	}
	if got := responses["sleep-1"].Error; got == nil || got.Code != "cancelled" {
		t.Fatalf("sleep request was not cancelled: %+v", responses["sleep-1"])
	}
}

func TestDuplicateActiveIDIsRejected(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	request := Request{
		Version: Version,
		ID:      "duplicate",
		Method:  "sleep",
		Params:  json.RawMessage(`{"milliseconds":50}`),
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

	h.send(t, Request{Version: Version + 1, ID: "version-1", Method: "health"})
	version := h.readResponse(t)
	if version.Error == nil || version.Error.Code != "unsupported_version" {
		t.Fatalf("unexpected version response: %+v", version)
	}

	h.send(t, Request{Version: Version, ID: "health-after-error", Method: "health"})
	health := h.readResponse(t)
	if health.Error != nil || health.ID != "health-after-error" {
		t.Fatalf("server did not recover after invalid input: %+v", health)
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

	h.send(t, Request{Version: Version, ID: "health-after-large", Method: "health"})
	health := h.readResponse(t)
	if health.Error != nil || health.ID != "health-after-large" {
		t.Fatalf("server did not recover after oversized input: %+v", health)
	}
}

func TestMessageLargerThanReaderBufferIsAccepted(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	value := strings.Repeat("x", 8*1024)
	params, err := json.Marshal(map[string]string{"value": value})
	if err != nil {
		t.Fatal(err)
	}
	h.send(t, Request{
		Version: Version,
		ID:      "large-valid",
		Method:  "echo",
		Params:  params,
	})

	response := h.readResponse(t)
	if response.Error != nil || response.ID != "large-valid" {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestTCC2Probe(t *testing.T) {
	h := newHarness(t, func(server *Server) {
		server.ProbeTCC2 = func(context.Context) (tcc2.ProbeResult, error) {
			return tcc2.ProbeResult{
				ServerName:       "taskchute-cloud-2",
				ServerVersion:    "1.0.0",
				ToolCount:        28,
				HasGetTaskChute:  true,
				ProtocolVersion:  "2025-06-18",
				ProcessExitClean: true,
			}, nil
		}
	})
	defer h.close(t)

	h.send(t, Request{Version: Version, ID: "tcc2-1", Method: "tcc2_probe"})
	response := h.readResponse(t)
	if response.Error != nil || response.ID != "tcc2-1" {
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
	request := Request{Version: Version, ID: "health-1", Method: "health"}
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
		t.Fatalf("expected ready and health response, got %d", len(responses))
	}
	var response Response
	if err := json.Unmarshal(responses[1], &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != "health-1" || response.Error != nil {
		t.Fatalf("unexpected response: %+v", response)
	}
}

func TestEOFStopsActiveRequest(t *testing.T) {
	request := Request{
		Version: Version,
		ID:      "sleep-1",
		Method:  "sleep",
		Params:  json.RawMessage(`{"milliseconds":5000}`),
	}
	payload, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	input := bytes.NewBuffer(append(payload, '\n'))
	var output lockedBuffer
	server := NewServer(input, &output, nil)

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

func TestManyConcurrentRequests(t *testing.T) {
	h := newHarness(t, nil)
	defer h.close(t)

	const count = 250
	for i := range count {
		h.send(t, Request{
			Version: Version,
			ID:      fmt.Sprintf("health-%03d", i),
			Method:  "health",
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
