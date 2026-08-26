package rules

import (
	"fmt"
	"sort"
	"strings"

	"github.com/takets/tcc-local-connector/internal/config"
)

type Plan struct {
	CycleID              int64           `json:"cycle_id"`
	DryRun               bool            `json:"dry_run"`
	Actions              []PlannedAction `json:"actions"`
	EnforceStopBundleIDs []string        `json:"enforce_stop_bundle_ids"`
}
type PlannedAction struct {
	ActionID       string            `json:"action_id"`
	Phase          string            `json:"phase"`
	Kind           string            `json:"kind"`
	BundleID       string            `json:"bundle_id,omitempty"`
	ProcessID      string            `json:"process_id,omitempty"`
	Executable     string            `json:"executable,omitempty"`
	Args           []string          `json:"args,omitempty"`
	WorkingDir     string            `json:"working_dir,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	GraceSeconds   int               `json:"grace_seconds,omitempty"`
	TimeoutSeconds int               `json:"timeout_seconds,omitempty"`
	Shell          bool              `json:"shell,omitempty"`
	Title          string            `json:"title,omitempty"`
	Message        string            `json:"message,omitempty"`
	Reason         string            `json:"reason"`
	Priority       int               `json:"priority"`
}
type Conflict struct {
	Target   string `json:"target"`
	Priority int    `json:"priority"`
}

func BuildPlan(cycleID int64, previous, current Evaluation) (Plan, []Conflict) {
	return buildPlan(cycleID, nil, nil, previous, current)
}

func BuildPlanWithDefault(cycleID int64, defaultActions []config.Action, previous, current Evaluation) (Plan, []Conflict) {
	return buildPlan(cycleID, defaultActions, nil, previous, current)
}

func BuildPlanWithLifecycle(cycleID int64, defaultActions, stopActions []config.Action, previous, current Evaluation) (Plan, []Conflict) {
	return buildPlan(cycleID, defaultActions, stopActions, previous, current)
}

func buildPlan(cycleID int64, defaultActions, stopActions []config.Action, previous, current Evaluation) (Plan, []Conflict) {
	selected := actions(defaultActions, "default.on_task_start", 0, "default")
	ruleCandidates := []PlannedAction{}
	for _, id := range sortedRuleIDs(current.ActiveRuleIDs) {
		rule := current.Rules[id]
		ruleCandidates = append(ruleCandidates, actions(rule.Ensure, "ensure", rule.Priority, "rules")...)
		if !previous.ActiveRuleIDs[id] {
			ruleCandidates = append(ruleCandidates, actions(rule.OnEnter, "on_enter", rule.Priority, "rules")...)
		}
	}
	for _, id := range sortedRuleIDs(previous.ActiveRuleIDs) {
		if !current.ActiveRuleIDs[id] {
			rule := previous.Rules[id]
			ruleCandidates = append(ruleCandidates, actions(rule.OnExit, "on_exit", rule.Priority, "rules")...)
		}
	}
	rules, conflicts := dedupe(ruleCandidates)
	selected = append(selected, rules...)
	selected = append(selected, actions(stopActions, "stop.on_task_end", 0, "stop")...)
	for index := range selected {
		selected[index].ActionID = fmt.Sprintf("%d-%d", cycleID, index+1)
	}
	// Why: Union distinct bundle IDs like browser.block domains, instead of
	// treating later app.stop entries as replacements of the first.
	seen := map[string]struct{}{}
	stops := make([]string, 0)
	for _, action := range selected {
		if action.Kind != "app.stop" || action.BundleID == "" {
			continue
		}
		if _, ok := seen[action.BundleID]; ok {
			continue
		}
		seen[action.BundleID] = struct{}{}
		stops = append(stops, action.BundleID)
	}
	sort.Strings(stops)
	return Plan{CycleID: cycleID, Actions: selected, EnforceStopBundleIDs: stops}, conflicts
}
func actions(values []config.Action, reason string, priority int, phase string) []PlannedAction {
	output := []PlannedAction{}
	for _, value := range values {
		if value.Type == "app.stop" {
			for _, id := range appStopBundleIDs(value) {
				output = append(output, PlannedAction{Phase: phase, Kind: "app.stop", BundleID: id, GraceSeconds: value.GraceSeconds, Reason: reason, Priority: priority})
			}
			continue
		}
		if value.Type != "app.start" && value.Type != "process.start" && value.Type != "process.stop" && value.Type != "command.run" && value.Type != "notify" {
			continue
		}
		output = append(output, PlannedAction{Phase: phase, Kind: value.Type, BundleID: value.BundleID, ProcessID: value.ProcessID, Executable: value.Executable, Args: value.Args, WorkingDir: value.WorkingDir, Env: value.Env, GraceSeconds: value.GraceSeconds, TimeoutSeconds: value.TimeoutSeconds, Shell: value.Shell, Title: value.Title, Message: value.Message, Reason: reason, Priority: priority})
	}
	return output
}

func appStopBundleIDs(value config.Action) []string {
	if len(value.BundleIDs) > 0 {
		return append([]string(nil), value.BundleIDs...)
	}
	if value.BundleID != "" {
		return []string{value.BundleID}
	}
	return nil
}
func targetKey(action PlannedAction) string {
	if action.Kind == "notify" {
		return action.Kind + ":" + action.Title
	}
	if action.Kind == "command.run" {
		return action.Kind + ":" + action.Executable + "\x00" + strings.Join(action.Args, "\x00") + "\x00" + action.WorkingDir
	}
	if action.ProcessID != "" {
		return "process:" + action.ProcessID
	}
	return "app:" + action.BundleID
}
func dedupe(actions []PlannedAction) ([]PlannedAction, []Conflict) {
	byTarget := map[string]PlannedAction{}
	conflicted := map[string]bool{}
	conflicts := []Conflict{}
	for _, action := range actions {
		key := targetKey(action)
		existing, ok := byTarget[key]
		if !ok || action.Priority > existing.Priority {
			byTarget[key] = action
			continue
		}
		if action.Priority == existing.Priority && action.Kind != existing.Kind {
			if !conflicted[key] {
				conflicts = append(conflicts, Conflict{Target: key, Priority: action.Priority})
				conflicted[key] = true
			}
			delete(byTarget, key)
		}
	}
	output := []PlannedAction{}
	for key, action := range byTarget {
		if !conflicted[key] {
			output = append(output, action)
		}
	}
	sort.SliceStable(output, func(i, j int) bool {
		if output[i].Priority != output[j].Priority {
			return output[i].Priority > output[j].Priority
		}
		return targetKey(output[i]) < targetKey(output[j])
	})
	return output, conflicts
}

func sortedRuleIDs(ids map[string]bool) []string {
	output := make([]string, 0, len(ids))
	for id := range ids {
		output = append(output, id)
	}
	sort.Strings(output)
	return output
}
