package tcc2

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestExchange(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"taskchute-cloud-2","version":"1.0.0"}}}`,
		`{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_taskchute"},{"name":"get_user"}]}}`,
		"",
	}, "\n")
	var output bytes.Buffer

	result, err := exchange(
		context.Background(),
		json.NewEncoder(&output),
		bufio.NewReader(strings.NewReader(input)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.ServerName != "taskchute-cloud-2" ||
		result.ServerVersion != "1.0.0" ||
		result.ProtocolVersion != "2025-06-18" ||
		result.ToolCount != 2 ||
		!result.HasGetTaskChute {
		t.Fatalf("unexpected probe result: %+v", result)
	}

	scanner := bufio.NewScanner(bytes.NewReader(output.Bytes()))
	var messages []map[string]any
	for scanner.Scan() {
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatal(err)
		}
		messages = append(messages, message)
	}
	if len(messages) != 3 {
		t.Fatalf("got %d outbound messages, want 3", len(messages))
	}
	if messages[0]["method"] != "initialize" ||
		messages[1]["method"] != "notifications/initialized" ||
		messages[2]["method"] != "tools/list" {
		t.Fatalf("unexpected outbound messages: %+v", messages)
	}
}

func TestExchangeRejectsUnexpectedID(t *testing.T) {
	input := `{"jsonrpc":"2.0","id":99,"result":{}}` + "\n"
	_, err := exchange(
		context.Background(),
		json.NewEncoder(&bytes.Buffer{}),
		bufio.NewReader(strings.NewReader(input)),
	)
	if err == nil || !strings.Contains(err.Error(), "unexpected response id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReadBoundedLineRejectsLargeResponse(t *testing.T) {
	_, err := readBoundedLine(
		bufio.NewReader(strings.NewReader(strings.Repeat("x", 32)+"\n")),
		16,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}
