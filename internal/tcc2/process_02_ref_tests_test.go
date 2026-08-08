package tcc2

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

func TestParseTaskChuteText_Golden(t *testing.T) {
	fixture := filepath.Join("testdata", "golden-taskchute.txt")
	text, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := ParseTaskChuteText(string(text), false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.RunningTasks) != 1 {
		t.Fatalf("want 1 running task, got=%d", len(result.RunningTasks))
	}
}

func TestParse_NoRunningTask(t *testing.T) {
	text := "## 2026-08-07\n- [Done] done 1\n- [Todo] todo 1"
	got, err := ParseTaskChuteText(text, false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(got.RunningTasks) != 0 {
		t.Fatalf("expected 0 running tasks, got=%d", len(got.RunningTasks))
	}
}

func TestParse_UnrecognizedFormat(t *testing.T) {
	if _, err := ParseTaskChuteText("not task lines\n", false); err == nil || !strings.Contains(err.Error(), "unrecognized_format") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_APIError(t *testing.T) {
	if _, err := ParseTaskChuteText("anything\n", true); err == nil || !strings.Contains(err.Error(), "tcc2_api_error") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_EmptyContent(t *testing.T) {
	if _, err := ParseTaskChuteText("", false); err == nil || !strings.Contains(err.Error(), "empty_content") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_TooManyRunningTasks(t *testing.T) {
	text := strings.Repeat("- [In Progress] task\n", constants.MaxRunningTasks+1)
	if _, err := ParseTaskChuteText(text, false); err == nil || !strings.Contains(err.Error(), "too_many_running_tasks") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParse_BracketInName(t *testing.T) {
	text := "- [In Progress] task [alpha] with bracket [ID: task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, Section: A]\n"
	result, err := ParseTaskChuteText(text, false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.RunningTasks) != 1 {
		t.Fatalf("expected running task, got=%d", len(result.RunningTasks))
	}
	if !strings.Contains(result.RunningTasks[0].Name, "task [alpha] with bracket") {
		t.Fatalf("name should preserve brackets, got=%q", result.RunningTasks[0].Name)
	}
}

func TestParse_LeadingSpacePreserved(t *testing.T) {
	text := "- [In Progress]   leading-space-task (09:10) [ID: task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, Section: A]\n"
	result, err := ParseTaskChuteText(text, false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.RunningTasks) != 1 || !strings.HasPrefix(result.RunningTasks[0].Name, "  leading-space-task") {
		t.Fatalf("name=%q", result.RunningTasks[0].Name)
	}
}

func TestParse_InvalidTaskIDFormat(t *testing.T) {
	text := "- [In Progress] invalid task [ID: bad-id, Section: A]\n"
	result, err := ParseTaskChuteText(text, false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.RunningTasks) != 1 {
		t.Fatalf("expected running task, got=%d", len(result.RunningTasks))
	}
	if !contains(result.RunningTasks[0].Warnings, "task_id_format") {
		t.Fatalf("expected task_id_format warning, got=%v", result.RunningTasks[0].Warnings)
	}
}

func TestParse_UnknownIDKey(t *testing.T) {
	text := "- [In Progress] task (09:10) [ID: task_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa, Section: A, Foo: bar]\n"
	result, err := ParseTaskChuteText(text, false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if len(result.RunningTasks) != 1 {
		t.Fatalf("expected running task, got=%d", len(result.RunningTasks))
	}
	if !contains(result.RunningTasks[0].Warnings, "unknown_id_key:Foo") {
		t.Fatalf("expected unknown_id_key warning, got=%v", result.RunningTasks[0].Warnings)
	}
}

func TestParseUserText_Golden(t *testing.T) {
	text := "- **Timezone:** Asia/Tokyo\n- **Start of Day:** -05:00:00\n- **Default View ID:** v1\n"
	got := parseUserText(text)
	if got.Timezone != "Asia/Tokyo" || got.StartOfDay != "-05:00:00" || got.DefaultViewID != "v1" {
		t.Fatalf("unexpected user info: %#v", got)
	}
}

func TestResolveCLIVersion_Homebrew(t *testing.T) {
	TestProbeHelpersAndVersion(t)
}

func TestResolveCLIVersion_NonHomebrew(t *testing.T) {
	if got := ResolveCLIVersion("/bin/echo"); got != "" {
		t.Fatalf("expected empty version, got=%q", got)
	}
}

func TestOpen_Success(t *testing.T) {
	TestSessionOpenCallClose(t)
}

func TestCall_ResponseTooLarge(t *testing.T) {
	heavy := strings.Repeat("x", constants.MCPMaxResponseBytes+1)
	resp := `{"jsonrpc":"2.0","id":1,"result":{"content":[{"type":"text","text":"` + heavy + `"}]}}
`
	_, err := readResponse(context.Background(), bufio.NewReader(strings.NewReader(resp)), 1)
	if err == nil {
		t.Fatal("expected oversized read error")
	}
}

func TestCall_ContextTimeout(t *testing.T) {
	s := &Session{input: nopWriteCloser{}, output: neverEndingReader(), cmd: nil}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.Call(ctx, "get_taskchute", map[string]any{}); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestFetchTaskChute_RefreshesExpiredUserInfo(t *testing.T) {
	base := t.TempDir()
	counterFile := filepath.Join(base, "calls.txt")
	script := filepath.Join(base, "fake-tcc2")
	program := shellScript(
		"COUNTER_FILE_PATH", counterFile,
		"#!/bin/sh\n"+
			`COUNTER_FILE='COUNTER_FILE_PATH'`+"\n"+
			`read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
while IFS= read -r line; do
	if echo "$line" | grep -q '"name":"get_user"'; then
		echo user >> "$COUNTER_FILE"
		printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\n- **Default View ID:** v1\n- **Start of Day:** 00:00:00"}]}}'
		continue
	fi
	if echo "$line" | grep -q '"name":"get_taskchute"'; then
		printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"## 2026-08-07\n- [Done] x"}]}}'
		continue
	fi
done
`)
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	userCache.Lock()
	userCache.value = cachedUser{info: UserInfo{Timezone: ""}, at: time.Now().Add(-(constants.UserInfoTTLSeconds + 1) * time.Second)}
	userCache.Unlock()
	if _, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	countRaw, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(countRaw), "user") != 1 {
		t.Fatalf("expected stale cache to trigger one get_user call, file=%q", string(countRaw))
	}
}

func TestFetchTaskChute_UserInfoFallback(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
read line
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"error":{"code":-1,"message":"auth"}}'
read line
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"## 2026-08-07\n- [Done] x"}]}}'
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()
	result, err := FetchTaskChute(context.Background(), script, nil, nil)
	if err != nil {
		t.Fatalf("fetch failed: %v", err)
	}
	if len(result.Warnings) == 0 {
		t.Fatal("expected fallback warning")
	}
}

func TestFetchTaskChute_ClosesSessionPerCycle(t *testing.T) {
	counterFile := filepath.Join(t.TempDir(), "pids.txt")
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := shellScript(
		"COUNTER_FILE_PATH", counterFile,
		"#!/bin/sh\n"+
			`COUNTER_FILE='COUNTER_FILE_PATH'`+"\n"+
			`echo $$ >> "$COUNTER_FILE"
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
read line
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\n- **Default View ID:** v1\n- **Start of Day:** 00:00:00"}]}}'
read line
printf '%s\n' '{"jsonrpc":"2.0","id":3,"result":{"content":[{"type":"text","text":"## 2026-08-07\n- [Done] x"}]}}'
`)
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()
	if _, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil {
		t.Fatalf("fetch1 failed: %v", err)
	}
	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()
	if _, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil {
		t.Fatalf("fetch2 failed: %v", err)
	}
	raw, err := os.ReadFile(counterFile)
	if err != nil {
		t.Fatalf("read counter failed: %v", err)
	}
	if len(strings.Fields(string(raw))) != 2 {
		t.Fatalf("expected 2 fresh sessions, got=%q", strings.TrimSpace(string(raw)))
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func shellScript(marker, path, body string) string {
	quoted := shellSingleQuote(path)
	return strings.ReplaceAll(body, marker, quoted)
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func neverEndingReader() *bufio.Reader {
	return bufio.NewReader(neverEndingReaderType{})
}

func (neverEndingReaderType) Read(_ []byte) (int, error) {
	select {}
}

type neverEndingReaderType struct{}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(_ []byte) (int, error) { return 0, nil }
func (nopWriteCloser) Close() error                { return nil }
