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

func TestBuildPlan_OneAppStopBundleIDsExpandsPerID(t *testing.T) {
	cfg := config.Config{
		Version:    2,
		TaskSource: config.TaskSource{Type: "tcc2_mcp", Executable: "/bin/echo", Args: []string{"mcp"}},
		Polling:    config.Polling{IntervalSeconds: 60, TimeoutSeconds: 20, FailureGraceSeconds: 180, FailurePolicy: "release_controls"},
		Logging:    config.Logging{Level: "info", RetainDays: 14},
		Rules: []config.Rule{{ID: "stop-inv", Ensure: []config.Action{
			{Type: "app.stop", BundleIDs: []string{"com.tinyspeck.slackmacgap", "com.amazon.Lassen"}},
		}}},
	}
	validated, errs := config.Validate(&cfg)
	if len(errs) != 0 || validated == nil {
		t.Fatalf("validate=%#v %#v", validated, errs)
	}
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"stop-inv": true},
		Rules:         map[string]config.Rule{"stop-inv": validated.Rules[0]},
	}
	plan, conflicts := BuildPlan(1, Evaluation{}, current)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts=%#v", conflicts)
	}
	got := map[string]bool{}
	for _, action := range plan.Actions {
		if action.Kind != "app.stop" {
			t.Fatalf("unexpected action=%#v", action)
		}
		got[action.BundleID] = true
	}
	if len(plan.Actions) != 2 || !got["com.tinyspeck.slackmacgap"] || !got["com.amazon.Lassen"] {
		t.Fatalf("actions=%#v", plan.Actions)
	}
	want := []string{"com.amazon.Lassen", "com.tinyspeck.slackmacgap"}
	if fmt.Sprint(plan.EnforceStopBundleIDs) != fmt.Sprint(want) {
		t.Fatalf("enforce=%#v want=%#v", plan.EnforceStopBundleIDs, want)
	}
}

func TestBuildPlan_MultipleEnsureAppStopsAreAllPlanned(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"stop-inv": true},
		Rules: map[string]config.Rule{
			"stop-inv": {ID: "stop-inv", Ensure: []config.Action{
				{Type: "app.stop", BundleID: "com.tinyspeck.slackmacgap"},
				{Type: "app.stop", BundleID: "com.amazon.Lassen"},
			}},
		},
	}
	plan, conflicts := BuildPlan(1, Evaluation{}, current)
	if len(conflicts) != 0 {
		t.Fatalf("conflicts=%#v", conflicts)
	}
	got := map[string]bool{}
	for _, action := range plan.Actions {
		if action.Kind != "app.stop" {
			t.Fatalf("unexpected action=%#v", action)
		}
		got[action.BundleID] = true
	}
	if len(plan.Actions) != 2 || !got["com.tinyspeck.slackmacgap"] || !got["com.amazon.Lassen"] {
		t.Fatalf("actions=%#v", plan.Actions)
	}
	want := []string{"com.amazon.Lassen", "com.tinyspeck.slackmacgap"}
	if fmt.Sprint(plan.EnforceStopBundleIDs) != fmt.Sprint(want) {
		t.Fatalf("enforce=%#v want=%#v", plan.EnforceStopBundleIDs, want)
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
				{Type: "app.stop", BundleID: "com.example.stop"},
				{Type: "process.start", ProcessID: "worker-start", Executable: "/bin/echo"},
				{Type: "command.run", Executable: "/bin/echo"},
				{Type: "process.stop", ProcessID: "worker-stop"},
				{Type: "notify", Title: "hello", Message: "world"},
			}},
		},
	}
	plan, _ := BuildPlan(1, Evaluation{}, current)
	if len(plan.Actions) != 6 {
		t.Fatalf("unexpected actions=%#v", plan.Actions)
	}
	seen := map[string]bool{}
	for _, action := range plan.Actions {
		seen[action.Kind] = true
	}
	for _, kind := range []string{"app.start", "app.stop", "process.start", "process.stop", "command.run", "notify"} {
		if !seen[kind] {
			t.Fatalf("plan is missing supported kind %q: %#v", kind, plan.Actions)
		}
	}
	for _, action := range plan.Actions {
		if action.Phase != "rules" {
			t.Fatalf("phase=%q action=%#v", action.Phase, action)
		}
	}
}

func TestBuildPlanWithDefault_SequencesPhasesWithoutCrossPhaseDedupe(t *testing.T) {
	defaultActions := []config.Action{
		{Type: "notify", Title: "first", Message: "default"},
		{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"},
	}
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true},
		Rules: map[string]config.Rule{"r": {ID: "r", Priority: 10, Ensure: []config.Action{
			{Type: "process.start", ProcessID: "worker", Executable: "/bin/echo"},
			{Type: "notify", Title: "second", Message: "rules"},
		}}},
	}

	plan, conflicts := BuildPlanWithDefault(9, defaultActions, Evaluation{}, current)
	if len(conflicts) != 0 || len(plan.Actions) != 4 {
		t.Fatalf("plan=%#v conflicts=%#v", plan, conflicts)
	}
	wantIDs := []string{"9-1", "9-2", "9-3", "9-4"}
	for i, action := range plan.Actions {
		if action.ActionID != wantIDs[i] {
			t.Fatalf("action[%d] id=%q want=%q", i, action.ActionID, wantIDs[i])
		}
	}
	if plan.Actions[0].Phase != "default" || plan.Actions[1].Phase != "default" || plan.Actions[2].Phase != "rules" || plan.Actions[3].Phase != "rules" {
		t.Fatalf("phases=%#v", plan.Actions)
	}
	if plan.Actions[0].Reason != "default.on_task_start" || plan.Actions[1].Reason != "default.on_task_start" {
		t.Fatalf("default reasons=%#v", plan.Actions[:2])
	}
	processPhases := map[string]bool{}
	for _, action := range plan.Actions {
		if action.ProcessID == "worker" {
			processPhases[action.Phase] = true
		}
	}
	if !processPhases["default"] || !processPhases["rules"] {
		t.Fatalf("cross-phase action was not retained: %#v", plan.Actions)
	}
}

func TestBuildPlanWithDefault_SkipsBrowserBlock(t *testing.T) {
	plan, _ := BuildPlanWithDefault(1, []config.Action{
		{Type: "browser.block", Domains: []string{"example.com"}},
		{Type: "notify", Title: "hello", Message: "world"},
	}, Evaluation{}, Evaluation{})
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "notify" || plan.Actions[0].Phase != "default" {
		t.Fatalf("actions=%#v", plan.Actions)
	}
}

func TestPlan_NoBrowserBlockInActions(t *testing.T) {
	current := Evaluation{
		ActiveRuleIDs: map[string]bool{"r": true},
		Rules: map[string]config.Rule{
			"r": {ID: "r", Ensure: []config.Action{
				{Type: "browser.block", Domains: []string{"example.com"}},
				{Type: "notify", Title: "hello", Message: "world"},
			}},
		},
	}
	plan, _ := BuildPlan(1, Evaluation{}, current)
	if len(plan.Actions) != 1 || plan.Actions[0].Kind != "notify" {
		t.Fatalf("actions=%#v", plan.Actions)
	}
}

func TestBuildBrowserPolicy_UnionSortedUnique(t *testing.T) {
	firstDomains := []string{"z.example", "a.example"}
	secondDomains := []string{"m.example", "a.example"}
	evaluation := Evaluation{
		ActiveRuleIDs: map[string]bool{"first": true, "second": true, "inactive": false},
		Rules: map[string]config.Rule{
			"first":    {ID: "first", Ensure: []config.Action{{Type: "browser.block", Domains: firstDomains}}},
			"second":   {ID: "second", Ensure: []config.Action{{Type: "browser.block", Domains: secondDomains}}},
			"inactive": {ID: "inactive", Ensure: []config.Action{{Type: "browser.block", Domains: []string{"ignored.example"}}}},
		},
	}
	got := BuildBrowserPolicy(evaluation)
	want := []string{"a.example", "m.example", "z.example"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("domains=%#v", got)
	}
	if fmt.Sprint(firstDomains) != "[z.example a.example]" || fmt.Sprint(secondDomains) != "[m.example a.example]" {
		t.Fatalf("input mutated: first=%#v second=%#v", firstDomains, secondDomains)
	}
	got[0] = "changed.example"
	if firstDomains[0] != "z.example" || secondDomains[0] != "m.example" {
		t.Fatalf("output aliases input: first=%#v second=%#v", firstDomains, secondDomains)
	}
}

func TestBuildBrowserPolicy_Empty(t *testing.T) {
	got := BuildBrowserPolicy(Evaluation{ActiveRuleIDs: map[string]bool{"r": true}, Rules: map[string]config.Rule{"r": {ID: "r"}}})
	if got == nil || len(got) != 0 {
		t.Fatalf("domains=%#v", got)
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
