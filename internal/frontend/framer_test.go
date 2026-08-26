package frontend

import "testing"

func TestLineFramerSplitsAcrossChunks(t *testing.T) {
	var framer LineFramer
	first := framer.Push([]byte("{\"event\":\"ready\"}"))
	if len(first) != 0 {
		t.Fatalf("partial line = %#v", first)
	}
	lines := framer.Push([]byte("\n{\"id\":\"1\"}\n"))
	if len(lines) != 2 || string(lines[0]) != "{\"event\":\"ready\"}" || string(lines[1]) != "{\"id\":\"1\"}" {
		t.Fatalf("lines = %#v", lines)
	}
}
