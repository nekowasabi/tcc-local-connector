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

func TestProbeHelpersAndVersion(t *testing.T) {
	if _, err := Probe(context.Background(), ""); err == nil {
		t.Fatal("empty executable accepted")
	}
	var b limitedBuffer
	payload := strings.Repeat("x", 5000)
	if n, err := b.Write([]byte(payload)); err != nil || n != len(payload) || b.Len() != 4096 || len(b.String()) != 4096 {
		t.Fatalf("buffer n=%d err=%v len=%d", n, err, b.Len())
	}
	if ResolveCLIVersion("/missing") != "" || ResolveAuthStatus(context.Background(), "/missing") {
		t.Fatal("missing CLI accepted")
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "Cellar", "tcc2", "1.2.3", "bin", "tcc2")
	if err := os.MkdirAll(filepath.Dir(real), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(real, []byte(""), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(dir, "cli")); err != nil {
		t.Fatal(err)
	}
	if got := ResolveCLIVersion(filepath.Join(dir, "cli")); got != "1.2.3" {
		t.Fatalf("version=%q", got)
	}
}

func TestReadResponseErrorsAndEOF(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := readResponse(ctx, bufio.NewReader(strings.NewReader("")), 1); err == nil {
		t.Fatal("cancel not observed")
	}
	if _, err := readResponse(context.Background(), bufio.NewReader(strings.NewReader("not json\n")), 1); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, err := readResponse(context.Background(), bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":1}`+"\n")), 1); err == nil {
		t.Fatal("null result accepted")
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

func TestProbeSuccessAndExitFailure(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1.0"}}}'
read line
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"tools":[{"name":"get_taskchute"},{"name":"other"}]}}'
exit ${FAKE_EXIT:-0}
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := Probe(context.Background(), script); err != nil || got.ServerName != "fake" || got.ToolCount != 2 || !got.HasGetTaskChute || !got.ProcessExitClean {
		t.Fatalf("Probe success = %#v %v", got, err)
	}
}

func TestProbeRejectsMalformedAndMCPError(t *testing.T) {
	for _, response := range []string{"not-json", `{"jsonrpc":"2.0","id":1,"error":{"code":-1}}`} {
		script := filepath.Join(t.TempDir(), "fake-tcc2")
		program := "#!/bin/sh\nread line\nprintf '%s\\n' '" + response + "'\n"
		if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := Probe(context.Background(), script); err == nil {
			t.Fatalf("Probe accepted %q", response)
		}
	}
}

func TestReadBoundedLineBranches(t *testing.T) {
	if got, err := readBoundedLine(bufio.NewReader(strings.NewReader("abc")), 10); err != nil || string(got) != "abc" {
		t.Fatalf("EOF line = %q %v", got, err)
	}
	if _, err := readBoundedLine(bufio.NewReader(strings.NewReader(strings.Repeat("x", 20)+"\n")), 10); err == nil {
		t.Fatal("oversized line accepted")
	}
	if _, err := readResponse(context.Background(), bufio.NewReader(strings.NewReader(`{"jsonrpc":"2.0","id":2,"result":{}}`+"\n")), 1); err == nil {
		t.Fatal("unexpected response id accepted")
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
