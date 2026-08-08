package rules

import (
	"fmt"
	"math/rand"
	"sort"
	"testing"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

func TestEvaluateMatchesContainsNotContainsCaseInsensitive(t *testing.T) {
	cfg := &config.Config{Rules: []config.Rule{
		{ID: "empty", Match: config.Match{}},
		{ID: "yes", Match: config.Match{TaskNameContains: []string{"BUILD"}}},
		{ID: "no", Match: config.Match{TaskNameNotContains: []string{"skip"}}},
		{ID: "blocked", Match: config.Match{TaskNameContains: []string{"build"}, TaskNameNotContains: []string{"skip"}}},
	}}
	e := Evaluate(cfg, []tcc2.RunningTask{{Name: "nightly Build"}, {Name: "skip this"}})
	if !e.ActiveRuleIDs["empty"] || !e.ActiveRuleIDs["yes"] || e.ActiveRuleIDs["no"] || e.ActiveRuleIDs["blocked"] {
		t.Fatalf("evaluation=%#v", e.ActiveRuleIDs)
	}
}

func TestBuildPlanTransitionsConflictsAndStopIDs(t *testing.T) {
	previous := Evaluation{
		ActiveRuleIDs: map[string]bool{"old": true},
		Rules: map[string]config.Rule{
			"old": {
				ID:       "old",
				Priority: 1,
				OnExit:   []config.Action{{Type: "app.stop", BundleID: "com.old"}},
			},
		},
	}
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"new": true},
		Rules: map[string]config.Rule{
			"new": {
				ID:       "new",
				Priority: 10,
				OnEnter: []config.Action{
					{Type: "notify", Title: "hello", Message: "world"},
				},
				Ensure: []config.Action{
					{Type: "app.stop", BundleID: "com.new"},
				},
			},
		},
	}
	plan, conflicts := BuildPlan(42, previous, current)
	if len(conflicts) != 0 || len(plan.Actions) != 3 || plan.Actions[0].ActionID != "42-1" || len(plan.EnforceStopBundleIDs) != 2 {
		t.Fatalf("plan=%#v conflicts=%#v", plan, conflicts)
	}
	if plan.EnforceStopBundleIDs[0] != "com.new" || plan.EnforceStopBundleIDs[1] != "com.old" {
		t.Fatalf("stops=%#v", plan.EnforceStopBundleIDs)
	}

	conflictRule := func(kind string) config.Rule {
		return config.Rule{ID: kind, Priority: 5, Ensure: []config.Action{{Type: kind, BundleID: "com.same", Title: "same", Message: "x"}}}
	}
	_ = conflictRule
	left := Evaluation{ActiveRuleIDs: map[string]bool{"a": true, "b": true}, Rules: map[string]config.Rule{
		"a": {ID: "a", Priority: 5, Ensure: []config.Action{{Type: "app.start", BundleID: "com.same"}}},
		"b": {ID: "b", Priority: 5, Ensure: []config.Action{{Type: "app.stop", BundleID: "com.same"}}},
	}}
	plan, conflicts = BuildPlan(1, Evaluation{ActiveRuleIDs: map[string]bool{}, Rules: map[string]config.Rule{}}, left)
	if len(plan.Actions) != 0 || len(conflicts) != 1 || conflicts[0].Priority != 5 {
		t.Fatalf("conflict plan=%#v conflicts=%#v", plan, conflicts)
	}
}

func TestBuildPlan_Priority(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"low": true, "high": true},
		Rules: map[string]config.Rule{
			"low":  {ID: "low", Priority: 10, Ensure: []config.Action{{Type: "app.stop", BundleID: "com.example.app"}}},
			"high": {ID: "high", Priority: 20, Ensure: []config.Action{{Type: "app.start", BundleID: "com.example.app"}}},
		},
	}
	plan, conflicts := BuildPlan(1, Evaluation{}, current)
	if len(conflicts) != 0 {
		t.Fatalf("unexpected conflicts=%#v", conflicts)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "app.start" || plan.Actions[0].BundleID != "com.example.app" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestBuildPlan_SamePriorityConflict(t *testing.T) {
	previous := Evaluation{ActiveRuleIDs: map[string]bool{}, Rules: map[string]config.Rule{}}
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r1": true, "r2": true, "other": true},
		Rules: map[string]config.Rule{
			"r1":    {ID: "r1", Priority: 1, Ensure: []config.Action{{Type: "app.stop", BundleID: "com.example.conflict"}}},
			"r2":    {ID: "r2", Priority: 1, Ensure: []config.Action{{Type: "app.start", BundleID: "com.example.conflict"}}},
			"other": {ID: "other", Priority: 2, Ensure: []config.Action{{Type: "notify", Title: "ok", Message: "ok"}}},
		},
	}
	plan, conflicts := BuildPlan(3, previous, current)
	if len(plan.Actions) != 1 || len(conflicts) != 1 {
		t.Fatalf("plan=%#v conflicts=%#v", plan, conflicts)
	}
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "notify" || plan.Actions[0].ActionID != "3-1" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestBuildPlan_EnterOnce(t *testing.T) {
	previous := Evaluation{ActiveRuleIDs: map[string]bool{}, Rules: map[string]config.Rule{
		"r": {ID: "r", Priority: 10, Match: config.Match{TaskNameContains: []string{"target"}}, OnEnter: []config.Action{{Type: "notify", Title: "entered", Message: "y"}}},
	}}
	current := Evaluation{ActiveRuleIDs: map[string]bool{"r": true}, Rules: map[string]config.Rule{
		"r": {ID: "r", Priority: 10, Match: config.Match{TaskNameContains: []string{"target"}}, OnEnter: []config.Action{{Type: "notify", Title: "entered", Message: "y"}}},
	}}
	plan, _ := BuildPlan(10, previous, current)
	if len(plan.Actions) != 1 || plan.Actions[0].Reason != "on_enter" {
		t.Fatalf("plan=%#v", plan)
	}
	plan, _ = BuildPlan(11, current, current)
	if len(plan.Actions) != 0 {
		t.Fatalf("plan should not re-enter=%#v", plan)
	}
}

func TestBuildPlan_EnsureIdempotent(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true},
		Rules: map[string]config.Rule{
			"r": {ID: "r", Priority: 10, Ensure: []config.Action{
				{Type: "app.start", BundleID: "com.example.app"},
				{Type: "notify", Title: "x", Message: "y"},
			}},
		},
	}
	plan1, _ := BuildPlan(10, Evaluation{}, current)
	plan2, _ := BuildPlan(10, Evaluation{}, current)
	if !equalPlanWithoutOrder(plan1, plan2) {
		t.Fatalf("plan1=%#v plan2=%#v", plan1, plan2)
	}
}

func TestBuildPlan_EnforceStopBundleIDsMatchActions(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true},
		Rules: map[string]config.Rule{
			"r": {ID: "r", Priority: 10, Ensure: []config.Action{
				{Type: "app.stop", BundleID: "com.example.stop"},
				{Type: "app.start", BundleID: "com.example.start"},
			}},
		},
	}
	plan, _ := BuildPlan(1, Evaluation{}, current)
	if len(plan.EnforceStopBundleIDs) != 1 || plan.EnforceStopBundleIDs[0] != "com.example.stop" {
		t.Fatalf("plan=%#v", plan)
	}
}

func TestBuildPlan_NoUnsupportedActions(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true},
		Rules: map[string]config.Rule{
			"r": {ID: "r", Priority: 10, Ensure: []config.Action{
				{Type: "app.start", BundleID: "com.example.app"},
				{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"},
				{Type: "command.run", Executable: "/bin/echo"},
				{Type: "process.stop", ProcessID: "worker"},
				{Type: "notify", Title: "hello", Message: "world"},
			}},
		},
	}
	plan, _ := BuildPlan(1, Evaluation{}, current)
	if len(plan.Actions) != 2 {
		t.Fatalf("unexpected actions=%#v", plan.Actions)
	}
	seen := map[string]bool{}
	for _, action := range plan.Actions {
		seen[action.Kind] = true
	}
	if seen["app.start"] != true || seen["notify"] != true || seen["process.start"] || seen["command.run"] || seen["process.stop"] {
		t.Fatalf("plan contains unsupported kinds=%#v", plan.Actions)
	}
}

func TestBuildPlan_Property(t *testing.T) {
	r := rand.New(rand.NewSource(2026_08_07))
	for i := 0; i < 100; i++ {
		current := randomizedEvaluation(r, i)
		planA, _ := BuildPlan(int64(i), Evaluation{}, current)
		planB, _ := BuildPlan(int64(i), Evaluation{}, current)
		if !equalPlanWithoutOrder(planA, planB) {
			t.Fatalf("property failed case=%d: %#v %#v", i, planA, planB)
		}
	}
}

func TestBuildPlan_ActionIDConsistency(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true, "s": true},
		Rules: map[string]config.Rule{
			"r": {ID: "r", Priority: 3, Ensure: []config.Action{{Type: "app.start", BundleID: "com.example.a"}}},
			"s": {ID: "s", Priority: 1, Ensure: []config.Action{{Type: "notify", Title: "x", Message: "y"}}},
		},
	}
	plan, _ := BuildPlan(7, Evaluation{}, current)
	got := make([]string, len(plan.Actions))
	for i := range plan.Actions {
		got[i] = plan.Actions[i].ActionID
	}
	sort.Strings(got)
	if got[0] != "7-1" || got[1] != "7-2" {
		t.Fatalf("action ids=%#v", got)
	}
}

func equalPlanWithoutOrder(a, b Plan) bool {
	if a.CycleID != b.CycleID || len(a.Actions) != len(b.Actions) {
		return false
	}
	for i := range a.Actions {
		if a.Actions[i].ActionID != b.Actions[i].ActionID || a.Actions[i].Kind != b.Actions[i].Kind || a.Actions[i].BundleID != b.Actions[i].BundleID || a.Actions[i].Title != b.Actions[i].Title || a.Actions[i].Message != b.Actions[i].Message || a.Actions[i].GraceSeconds != b.Actions[i].GraceSeconds || a.Actions[i].Priority != b.Actions[i].Priority {
			return false
		}
	}
	return true
}

func randomizedEvaluation(r *rand.Rand, seed int) Evaluation {
	ruleCount := r.Intn(3) + 1
	rules := map[string]config.Rule{}
	active := map[string]bool{}
	for i := 0; i < ruleCount; i++ {
		id := fmt.Sprintf("r%d", i)
		rules[id] = config.Rule{
			ID:       id,
			Priority: r.Intn(20),
			Ensure: []config.Action{{
				Type:     []string{"app.start", "app.stop", "notify", "app.start", "app.stop"}[r.Intn(5)],
				BundleID: "com.example." + id,
				Title:    "title",
				Message:  "message",
			}},
		}
		if r.Intn(2) == 0 {
			active[id] = true
		}
	}
	if len(active) == 0 {
		for id := range rules {
			active[id] = true
			break
		}
	}
	for id := range active {
		if r.Intn(3) == 0 {
			rule := rules[id]
			rule.Match = config.Match{TaskNameContains: []string{"x"}}
			rules[id] = rule
		}
	}
	return Evaluation{ActiveRuleIDs: active, Rules: rules}
}
