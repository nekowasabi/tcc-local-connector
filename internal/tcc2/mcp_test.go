package tcc2

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
)

func TestReadBoundedLineRejectsLargeResponse(t *testing.T) {
	_, err := readBoundedLine(
		bufio.NewReader(strings.NewReader(strings.Repeat("x", 32)+"\n")),
		16,
	)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSessionReadRejectsInvalidReplies(t *testing.T) {
	for name, tc := range map[string]struct {
		input string
		want  string
	}{
		"oversized":     {strings.Repeat("x", maxResponseBytes+1) + "\n", "exceeds maximum size"},
		"invalid json":  {"not json\n", "invalid character"},
		"error reply":   {`{"jsonrpc":"2.0","id":1,"error":{"code":-1}}` + "\n", "MCP response error"},
		"eof":           {"", "EOF"},
		"only other id": {`{"jsonrpc":"2.0","id":2,"result":{}}` + "\n", "EOF"},
	} {
		s := &Session{output: bufio.NewReader(strings.NewReader(tc.input))}
		if _, err := s.read(context.Background(), 1); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: err=%v, want %q", name, err, tc.want)
		}
	}
}

func TestSessionReadSkipsOtherIDs(t *testing.T) {
	input := `{"jsonrpc":"2.0","method":"notifications/x"}` + "\n" + `{"jsonrpc":"2.0","id":7,"result":{}}` + "\n" + `{"jsonrpc":"2.0","id":1,"result":{"ok":true}}` + "\n"
	s := &Session{output: bufio.NewReader(strings.NewReader(input))}
	got, err := s.read(context.Background(), 1)
	if err != nil || string(got) != `{"ok":true}` {
		t.Fatalf("got=%s err=%v", got, err)
	}
}

func TestSessionReadHonorsCancel(t *testing.T) {
	r, w := io.Pipe()
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &Session{output: bufio.NewReader(r)}
	if _, err := s.read(ctx, 1); err != context.Canceled {
		t.Fatalf("err=%v, want context.Canceled", err)
	}
}
