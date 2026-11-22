package conformance

// AccessGate.tla の定義が今のコードと一致することを確かめる適合テスト。
// ここが落ちたら、コードがモデルの「今のコード」の列（run.sh の code 変種と guards.py）から動いた合図。
// 意図した変更なら AccessGate.tla・run.sh の期待値・guards.py・このテストを一緒に直す（.claude/skills/verify-access-gate/SKILL.md）。

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/browserpolicy"
	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/engine"
	"github.com/takets/tcc-local-connector/internal/rules"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

const slackID = "com.tinyspeck.slackmacgap"

// モデルの TaskKinds。allowed は許可タスク、listed は今の config.yml の 12 パターンのどれか
var tasksOf = map[string][]tcc2.RunningTask{
	"none":    nil,
	"allowed": {{Name: "確認 sNs / X", TaskID: "task_00000000000000000000000000000001"}},
	"listed":  {{Name: "mtg / 定例", TaskID: "task_00000000000000000000000000000002"}},
	"other":   {{Name: "雑務", TaskID: "task_00000000000000000000000000000003"}},
}

func gateRule(match config.Match) config.Rule {
	return config.Rule{ID: "gate", Match: match, Ensure: []config.Action{
		{Type: "app.stop", BundleIDs: []string{slackID}},
		{Type: "browser.block", Domains: []string{"x.com"}},
	}}
}

// RuleActive(t): current は listed のときだけ、deny は allowed 以外で有効
func TestRuleActiveMatchesModel(t *testing.T) {
	configs := map[string]config.Match{
		"current": {TaskNameContains: []string{"mtg /"}},
		"deny":    {TaskNameNotContains: []string{"SNS /"}},
	}
	want := map[string]map[string]bool{
		"current": {"none": false, "allowed": false, "listed": true, "other": false},
		"deny":    {"none": true, "allowed": false, "listed": true, "other": true},
	}
	empty := rules.Evaluation{ActiveRuleIDs: map[string]bool{}, Rules: map[string]config.Rule{}}
	for name, match := range configs {
		cfg := &config.Config{Rules: []config.Rule{gateRule(match)}}
		for kind, tasks := range tasksOf {
			evaluation := rules.Evaluate(cfg, tasks)
			blocked := slices.Contains(rules.BuildBrowserPolicy(evaluation), "x.com")
			plan, _ := rules.BuildPlan(1, empty, evaluation)
			stops := slices.Contains(plan.EnforceStopBundleIDs, slackID)
			if blocked != want[name][kind] || stops != want[name][kind] {
				t.Errorf("%s/%s: browser.block=%v app.stop=%v, model RuleActive=%v", name, kind, blocked, stops, want[name][kind])
			}
		}
	}
}

func writeConfig(t *testing.T, dir, executable string) string {
	t.Helper()
	body := "version: 2\ntask_source:\n  executable: " + executable + "\npolling:\n  interval_seconds: 10\n  timeout_seconds: 5\n  failure_grace_seconds: 0\nsafety:\n  dry_run: false\nrules:\n  - id: gate\n    match:\n      task_name_not_contains: [\"SNS /\"]\n    ensure:\n      - type: app.stop\n        bundle_ids: [" + slackID + "]\n      - type: browser.block\n        domains: [x.com]\n"
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// tcc2 の代わり。dir/fail があれば取得に失敗する。そうでなければ「雑務」を実行中として返す
const fakeTCC2 = "#!/usr/bin/env bash\nset -euo pipefail\nif [ -f \"$(dirname \"$0\")/fail\" ]; then exit 1; fi\nwhile IFS= read -r line; do\n  case \"$line\" in *'\"method\":\"notifications/initialized\"'*) continue ;; esac\n  id=$(printf '%s\\n' \"$line\" | sed -nE 's/.*\"id\":([0-9]+).*/\\1/p')\n  case \"$line\" in\n    *'\"method\":\"initialize\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"protocolVersion\":\"2025-06-18\"}}\\n' \"$id\" ;;\n    *'\"name\":\"get_user\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"- **Timezone:** UTC\\\\n- **Start of Day:** -05:00:00\"}]}}\\n' \"$id\" ;;\n    *'\"name\":\"get_taskchute\"'*) printf '{\"jsonrpc\":\"2.0\",\"id\":%s,\"result\":{\"content\":[{\"type\":\"text\",\"text\":\"## 2026-08-07\\\\n- [In Progress] 雑務 [ID: task_0123456789abcdef0123456789abcdef]\"}]}}\\n' \"$id\" ;;\n  esac\ndone\n"

type plans struct {
	mu    sync.Mutex
	stops [][]string
}

func (p *plans) emit(name string, data any) {
	if plan, ok := data.(rules.Plan); ok && name == "plan" {
		p.mu.Lock()
		p.stops = append(p.stops, plan.EnforceStopBundleIDs)
		p.mu.Unlock()
	}
}
func (p *plans) waitLast(t *testing.T, n int) []string {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		p.mu.Lock()
		if len(p.stops) >= n {
			last := p.stops[len(p.stops)-1]
			p.mu.Unlock()
			return last
		}
		p.mu.Unlock()
	}
	t.Fatalf("plan event %d did not arrive", n)
	return nil
}

func readPolicy(t *testing.T, path string) browserpolicy.Policy {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := browserpolicy.Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	return policy
}

// Tick の 2 つの枝: 成功なら Eval（deny 設定で x.com と Slack を止める）、
// 猶予切れでも failClosed により許可タスクなしの Eval を適用する。
func TestEngineFailClosedOnGraceExpiry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	fake := filepath.Join(dir, "tcc2")
	if err := os.WriteFile(fake, []byte(fakeTCC2), 0o700); err != nil {
		t.Fatal(err)
	}
	policyPath := filepath.Join(dir, "browser-policy.json")
	recorder := &plans{}
	e := engine.New(engine.Deps{ConfigPath: writeConfig(t, dir, fake), BrowserPolicyPath: policyPath, Emit: recorder.emit})
	if result := e.LoadConfig(); !result.OK {
		t.Fatalf("deny config must pass validation (task_name_not_contains only): %+v", result.Errors)
	}
	if _, err := e.RunCycleNow(context.Background()); err != nil {
		t.Fatalf("success cycle: %v", err)
	}
	if policy := readPolicy(t, policyPath); !policy.Enforce || !slices.Contains(policy.Domains, "x.com") {
		t.Fatalf("success with a non-allowed task must enforce x.com: %+v", policy)
	}
	if stops := recorder.waitLast(t, 1); !slices.Contains(stops, slackID) {
		t.Fatalf("success with a non-allowed task must enforce-stop Slack: %v", stops)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := e.RunCycleNow(context.Background()); err == nil {
		t.Fatal("failing tcc2 must fail the cycle")
	}
	if got := e.Snapshot().State; got != "degraded" {
		t.Fatalf("grace expiry state = %s, want degraded", got)
	}
	if policy := readPolicy(t, policyPath); !policy.Enforce || !slices.Contains(policy.Domains, "x.com") {
		t.Fatalf("model (failClosed): grace expiry keeps x.com blocked, got %+v", policy)
	}
	if stops := recorder.waitLast(t, 2); !slices.Contains(stops, slackID) {
		t.Fatalf("model (failClosed): grace expiry enforces the Slack stop, got %v", stops)
	}
}
