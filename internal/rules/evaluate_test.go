package rules

import (
	"testing"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

func TestEvaluate_ContainsOr(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "combo", Match: config.Match{TaskNameContains: []string{"build", "run"}}},
		},
	}
	e := Evaluate(cfg, []tcc2.RunningTask{{Name: "nightly run"}, {Name: "other"}})
	if !e.ActiveRuleIDs["combo"] {
		t.Fatalf("contains OR not working")
	}
}

func TestEvaluate_NotContainsAndNot(t *testing.T) {
	cfg := &config.Config{
		Rules: []config.Rule{
			{ID: "combo", Match: config.Match{TaskNameContains: []string{"task"}, TaskNameNotContains: []string{"skip", "ignore"}}},
		},
	}
	e := Evaluate(cfg, []tcc2.RunningTask{{Name: "important task"}, {Name: "other"}})
	if !e.ActiveRuleIDs["combo"] {
		t.Fatalf("not contains AND not should be satisfied")
	}
	e = Evaluate(cfg, []tcc2.RunningTask{{Name: "skip task"}})
	if e.ActiveRuleIDs["combo"] {
		t.Fatalf("should not match when not_contains hit")
	}
}

func TestEvaluate_NoRunningTasks_Asymmetry(t *testing.T) {
	cfg := &config.Config{Rules: []config.Rule{
		{ID: "contains", Match: config.Match{TaskNameContains: []string{"x"}}},
		{ID: "not_contains", Match: config.Match{TaskNameNotContains: []string{"x"}}},
	}}
	e := Evaluate(cfg, []tcc2.RunningTask{})
	if e.ActiveRuleIDs["contains"] {
		t.Fatalf("contains must be false with no tasks")
	}
	if !e.ActiveRuleIDs["not_contains"] {
		t.Fatalf("not_contains must be true with no tasks")
	}
}

func TestEvaluate_EmptyMatchAlwaysTrue(t *testing.T) {
	cfg := &config.Config{Rules: []config.Rule{{ID: "all", Match: config.Match{}}}}
	e := Evaluate(cfg, []tcc2.RunningTask{})
	if !e.ActiveRuleIDs["all"] {
		t.Fatalf("empty match should be true")
	}
}

func TestEvaluate_CaseInsensitive(t *testing.T) {
	cfg := &config.Config{Rules: []config.Rule{
		{ID: "ci", Match: config.Match{TaskNameContains: []string{"bUiLd"}}},
	}}
	e := Evaluate(cfg, []tcc2.RunningTask{{Name: "nightly BUILD"}})
	if !e.ActiveRuleIDs["ci"] {
		t.Fatalf("case insensitive match failed")
	}
}
