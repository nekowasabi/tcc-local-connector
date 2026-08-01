package tcc2

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"
)

const (
	mcpProtocolVersion = "2025-06-18"
	maxResponseBytes   = 1024 * 1024
)

type ProbeResult struct {
	ServerName       string `json:"server_name"`
	ServerVersion    string `json:"server_version"`
	ToolCount        int    `json:"tool_count"`
	HasGetTaskChute  bool   `json:"has_get_taskchute"`
	ProtocolVersion  string `json:"protocol_version"`
	ProcessExitClean bool   `json:"process_exit_clean"`
}

type response struct {
	ID     int             `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func Probe(ctx context.Context, executable string) (ProbeResult, error) {
	if executable == "" {
		return ProbeResult{}, errors.New("tcc2 executable is required")
	}

	cmd := exec.CommandContext(ctx, executable, "mcp")
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("open tcc2 stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ProbeResult{}, fmt.Errorf("open tcc2 stdout: %w", err)
	}
	var stderr limitedBuffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return ProbeResult{}, fmt.Errorf("start tcc2 mcp: %w", err)
	}

	reader := bufio.NewReader(stdout)
	encoder := json.NewEncoder(stdin)
	result, probeErr := exchange(ctx, encoder, reader)
	_ = stdin.Close()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return ProbeResult{}, ctx.Err()
	}
	if probeErr != nil {
		return ProbeResult{}, probeErr
	}
	if waitErr != nil {
		return ProbeResult{}, fmt.Errorf("tcc2 mcp exit: %w (stderr bytes: %d)", waitErr, stderr.Len())
	}
	result.ProcessExitClean = true
	return result, nil
}

func exchange(ctx context.Context, encoder *json.Encoder, reader *bufio.Reader) (ProbeResult, error) {
	if err := encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "initialize",
		"params": map[string]any{
			"protocolVersion": mcpProtocolVersion,
			"capabilities":    map[string]any{},
			"clientInfo": map[string]string{
				"name":    "tcc-local-connector-wsl-probe",
				"version": "0.1.0",
			},
		},
	}); err != nil {
		return ProbeResult{}, fmt.Errorf("send initialize: %w", err)
	}

	initialize, err := readResponse(ctx, reader, 1)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("initialize: %w", err)
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	if err := json.Unmarshal(initialize.Result, &initResult); err != nil {
		return ProbeResult{}, fmt.Errorf("decode initialize result: %w", err)
	}

	if err := encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
		"params":  map[string]any{},
	}); err != nil {
		return ProbeResult{}, fmt.Errorf("send initialized notification: %w", err)
	}
	if err := encoder.Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "tools/list",
		"params":  map[string]any{},
	}); err != nil {
		return ProbeResult{}, fmt.Errorf("send tools/list: %w", err)
	}

	tools, err := readResponse(ctx, reader, 2)
	if err != nil {
		return ProbeResult{}, fmt.Errorf("tools/list: %w", err)
	}
	var toolsResult struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(tools.Result, &toolsResult); err != nil {
		return ProbeResult{}, fmt.Errorf("decode tools/list result: %w", err)
	}

	hasGetTaskChute := false
	for _, tool := range toolsResult.Tools {
		if tool.Name == "get_taskchute" {
			hasGetTaskChute = true
			break
		}
	}

	return ProbeResult{
		ServerName:      initResult.ServerInfo.Name,
		ServerVersion:   initResult.ServerInfo.Version,
		ToolCount:       len(toolsResult.Tools),
		HasGetTaskChute: hasGetTaskChute,
		ProtocolVersion: initResult.ProtocolVersion,
	}, nil
}

func readResponse(ctx context.Context, reader *bufio.Reader, expectedID int) (response, error) {
	type result struct {
		payload []byte
		err     error
	}
	done := make(chan result, 1)
	go func() {
		payload, err := readBoundedLine(reader, maxResponseBytes)
		done <- result{payload: payload, err: err}
	}()

	select {
	case <-ctx.Done():
		return response{}, ctx.Err()
	case result := <-done:
		if result.err != nil {
			return response{}, result.err
		}
		var decoded response
		if err := json.Unmarshal(result.payload, &decoded); err != nil {
			return response{}, fmt.Errorf("invalid JSON response: %w", err)
		}
		if decoded.ID != expectedID {
			return response{}, fmt.Errorf("unexpected response id %d, want %d", decoded.ID, expectedID)
		}
		if len(decoded.Error) > 0 && string(decoded.Error) != "null" {
			return response{}, fmt.Errorf("MCP error: %s", decoded.Error)
		}
		if len(decoded.Result) == 0 {
			return response{}, errors.New("MCP response has no result")
		}
		return decoded, nil
	}
}

func readBoundedLine(reader *bufio.Reader, limit int) ([]byte, error) {
	var output bytes.Buffer
	tooLarge := false
	for {
		fragment, err := reader.ReadSlice('\n')
		if !tooLarge {
			if output.Len()+len(fragment) > limit+1 {
				tooLarge = true
			} else {
				_, _ = output.Write(fragment)
			}
		}
		switch {
		case err == nil:
			if tooLarge {
				return nil, errors.New("MCP response exceeds maximum size")
			}
			return bytes.TrimSuffix(output.Bytes(), []byte{'\n'}), nil
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if tooLarge {
				return nil, errors.New("MCP response exceeds maximum size")
			}
			if output.Len() == 0 {
				return nil, io.EOF
			}
			return output.Bytes(), nil
		default:
			return nil, err
		}
	}
}

type limitedBuffer struct {
	buffer bytes.Buffer
}

func (b *limitedBuffer) Write(payload []byte) (int, error) {
	const limit = 4096
	remaining := limit - b.buffer.Len()
	if remaining > 0 {
		if len(payload) < remaining {
			remaining = len(payload)
		}
		_, _ = b.buffer.Write(payload[:remaining])
	}
	return len(payload), nil
}

func (b *limitedBuffer) String() string {
	return b.buffer.String()
}

func (b *limitedBuffer) Len() int {
	return b.buffer.Len()
}
