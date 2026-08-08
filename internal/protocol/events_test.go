package protocol

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestEmitWritesProtocolEvent(t *testing.T) {
	var output bytes.Buffer
	server := NewServer(bytes.NewReader(nil), &output, nil)
	server.Emit("plan", map[string]any{"cycle_id": 1})
	var event Event
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &event); err != nil {
		t.Fatal(err)
	}
	if event.Event != "event.plan" || event.Version != Version {
		t.Fatalf("event = %#v", event)
	}
}
