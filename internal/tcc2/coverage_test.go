package tcc2

import (
	"bufio"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseTaskChuteTextNormalAndErrors(t *testing.T) {
	text := "## 2026-08-07\n- [In Progress] Build Task (09:10, meta) [ID: task_0123456789abcdef0123456789abcdef, Section: Work, Project: P, Mode: M, Routine: R, Tags: x, Unknown: y]\n- [Done] Finished\n"
	got, err := ParseTaskChuteText(text, false)
	if err != nil || len(got.RunningTasks) != 1 || got.RunningTasks[0].TaskID == "" || got.RunningTasks[0].Section != "Work" || got.StatusCounts["Done"] != 1 {
		t.Fatalf("parsed=%#v err=%v", got, err)
	}
	for _, tc := range []struct {
		text    string
		isError bool
		code    string
	}{{"", false, "empty_content"}, {"anything", true, "tcc2_api_error"}, {"plain", false, "unrecognized_format"}} {
		_, err := ParseTaskChuteText(tc.text, tc.isError)
		if err == nil || !strings.Contains(err.Error(), tc.code) {
			t.Fatalf("%q: %v", tc.code, err)
		}
	}
	tooMany := strings.Repeat("- [In Progress] x\n", 33)
	if _, err := ParseTaskChuteText(tooMany, false); err == nil || !strings.Contains(err.Error(), "too_many_running_tasks") {
		t.Fatalf("too many error=%v", err)
	}
}

func TestParseHelpersAndUser(t *testing.T) {
	if got := parseUserText("- **Timezone:** Asia/Tokyo\n- **Start of Day:** 05:00\n- **Default View ID:** v1"); got.Timezone != "Asia/Tokyo" || got.DefaultViewID != "v1" {
		t.Fatalf("user=%#v", got)
	}
	if got := trimTaskName(strings.Repeat("あ", 300)); len(got) > 512 {
		t.Fatalf("name too long: %d", len(got))
	}
	if rest, id, section, _, _, _, warnings := splitIDBlock("Task [ID: task_0123456789abcdef0123456789abcdef, Section: S, Unknown]"); rest != "Task" || id == "" || section != "S" || len(warnings) == 0 {
		t.Fatalf("id block=%q %q %q %#v", rest, id, section, warnings)
	}
	if rest, start, meta := splitMetaGroup("Task (bad)"); rest != "Task (bad)" || start != "" || meta != "" {
		t.Fatalf("meta=%q %q %q", rest, start, meta)
	}
	if (&ParseError{Code: "x", Message: "y"}).Error() != "x: y" {
		t.Fatal("ParseError format")
	}
}

func TestSessionRequestCallAndJoinText(t *testing.T) {
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	s := &Session{input: inWriter, output: bufio.NewReader(outReader), cmd: nil}
	done := make(chan struct{})
	go func() {
		defer close(done)
		b := make([]byte, 256)
		_, _ = inReader.Read(b)
		_, _ = outWriter.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"ok"}]}}` + "\n"))
		_ = outWriter.Close()
	}()
	result, err := s.Call(context.Background(), "get_user", map[string]any{})
	if err != nil || len(result.Content) != 1 || joinText(result) != "ok" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	_ = inWriter.Close()
	<-done
}

func TestTaskchuteErrorPaths(t *testing.T) {
	if _, err := Open(context.Background(), "/missing/tcc2", nil); err == nil {
		t.Fatal("missing executable accepted")
	}
	if _, err := FetchTaskChute(context.Background(), "/missing/tcc2", nil, nil); err == nil {
		t.Fatal("missing executable accepted")
	}
	s := &Session{input: failingWriter{}, output: bufio.NewReader(strings.NewReader(""))}
	if _, err := s.request(context.Background(), "x", nil); err == nil {
		t.Fatal("request write succeeded")
	}
	if _, err := s.read(context.Background(), 1); err == nil {
		t.Fatal("read EOF accepted")
	}
}

func TestReadBoundedLineBranches(t *testing.T) {
	if got, err := readBoundedLine(bufio.NewReader(strings.NewReader("abc")), 10); err != nil || string(got) != "abc" {
		t.Fatalf("EOF line = %q %v", got, err)
	}
	if _, err := readBoundedLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", 20)+"\n")), 10); err == nil {
		t.Fatal("oversized line accepted")
	}
}

func TestFetchTaskChuteAndUserWithFakeServer(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
read line
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\n- **Default View ID:** v1"}]}}'
read line
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"## 2026-08-07\n- [Done] Finished"}]}}'
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()
	if got, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil || got.StatusCounts["Done"] != 1 {
		t.Fatalf("FetchTaskChute = %#v %v", got, err)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (failingWriter) Close() error              { return nil }
