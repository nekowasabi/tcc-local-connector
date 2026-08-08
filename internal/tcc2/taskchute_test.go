package tcc2

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestOpen_ExecutableNotFound(t *testing.T) {
	if _, err := Open(context.Background(), "/does/not/exist/tcc2", nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionOpenCallClose(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
exit 0
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), script, nil)
	if err != nil {
		t.Fatalf("open session failed: %v", err)
	}
	if session == nil || session.cmd == nil {
		t.Fatal("session command missing")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close failed: %v", err)
	}
}

func TestFetchTaskChute_UsesCachedUserInfo(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
while IFS= read -r line; do
	if ! printf '%s' "$line" | grep -q '"id"'; then
		continue
	fi

	id=$(printf '%s' "$line" | sed -E 's/.*"id"[[:space:]]*:[[:space:]]*([0-9]+).*/\1/')

	if printf '%s' "$line" | grep -q '"method":"initialize"'; then
		printf '%s\n' '{"jsonrpc":"2.0","id":'"$id"',"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
		continue
	fi

	if printf '%s' "$line" | grep -q '"name":"get_user"'; then
		printf '%s\n' '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"- **Timezone:** UTC\n- **Default View ID:** cached_view\n- **Start of Day:** 00:00:00\n"}]}}'
		continue
	fi

	if printf '%s' "$line" | grep -q '"name":"get_taskchute"'; then
		printf '%s\n' '{"jsonrpc":"2.0","id":'"$id"',"result":{"content":[{"type":"text","text":"## 2026-08-07\n- [Done] Finished"}]}}'
		continue
	fi
done

exit 0
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}

	userCache.Lock()
	userCache.value = cachedUser{}
	userCache.Unlock()

	if _, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if _, err := FetchTaskChute(context.Background(), script, nil, nil); err != nil {
		t.Fatalf("cached fetch failed: %v", err)
	}
}

func TestFetchUser_CanParseUserText(t *testing.T) {
	script := filepath.Join(t.TempDir(), "fake-tcc2")
	program := `#!/bin/sh
read line
printf '%s\n' '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-06-18","serverInfo":{"name":"fake","version":"1"}}}'
read line
printf '%s\n' '{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"- **Timezone:** Asia/Tokyo\n- **Start of Day:** -05:00:00\n"}]}}'
`
	if err := os.WriteFile(script, []byte(program), 0o700); err != nil {
		t.Fatal(err)
	}
	session, err := Open(context.Background(), script, nil)
	if err != nil {
		t.Fatalf("open session failed: %v", err)
	}
	defer session.Close()

	user, err := FetchUser(context.Background(), session)
	if err != nil {
		t.Fatalf("fetch user failed: %v", err)
	}
	if user.Timezone != "Asia/Tokyo" || user.StartOfDay != "-05:00:00" {
		t.Fatalf("user=%#v", user)
	}
}
