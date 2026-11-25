package rules

import (
	"strings"
	"testing"

	"github.com/takets/tcc-local-connector/internal/config"
)

func TestShortNotice(t *testing.T) {
	if got := shortNotice("  短い説明\n二行目は出さない"); got != "短い説明" {
		t.Fatalf("first line = %q", got)
	}
	long := strings.Repeat("あ", 100)
	got := []rune(shortNotice(long))
	if len(got) != noticeMaxRunes || string(got[len(got)-1]) != "…" {
		t.Fatalf("truncated = %q (%d runes)", string(got), len(got))
	}
}

func TestBuildPlan_StopNoticeNamesWinningRule(t *testing.T) {
	stop := []config.Action{{Type: "app.stop", BundleID: "com.slack"}}
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"low": true, "high": true},
		Rules: map[string]config.Rule{
			"low":  {ID: "low", Priority: 1, Description: "低い", Ensure: stop},
			"high": {ID: "high", Priority: 9, Description: "高い方の説明", Ensure: stop},
		},
	}
	plan, _ := BuildPlan(1, Evaluation{}, current)
	if len(plan.Actions) != 1 || plan.Actions[0].Title != "ロック中: high" || plan.Actions[0].Message != "高い方の説明" {
		t.Fatalf("actions=%#v", plan.Actions)
	}
}
