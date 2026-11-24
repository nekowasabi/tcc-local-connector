package rules

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

type Evaluation struct {
	ActiveRuleIDs map[string]bool        `json:"active_rule_ids"`
	Rules         map[string]config.Rule `json:"-"`
}

func Evaluate(cfg *config.Config, tasks []tcc2.RunningTask) Evaluation {
	evaluation := Evaluation{ActiveRuleIDs: map[string]bool{}, Rules: map[string]config.Rule{}}
	for _, rule := range cfg.Rules {
		evaluation.Rules[rule.ID] = rule
		if matchRule(rule, tasks) {
			evaluation.ActiveRuleIDs[rule.ID] = true
		}
	}
	return evaluation
}
func matchRule(rule config.Rule, tasks []tcc2.RunningTask) bool {
	contains := rule.Match.TaskNameContains
	notContains := rule.Match.TaskNameNotContains
	// Why: with both task_name_contains and overrun_ratio, the overrun must come from a
	// task the name matched; another running task's overrun must not trigger the rule.
	candidates := tasks
	if len(contains) > 0 {
		candidates = nil
		for _, task := range tasks {
			if containsAnyFold(task.Name, contains) {
				candidates = append(candidates, task)
			}
		}
		if len(candidates) == 0 {
			return false
		}
	}
	if rule.Match.OverrunRatio > 0 && !anyOverrun(candidates, rule.Match.OverrunRatio, now()) {
		return false
	}
	for _, task := range tasks {
		if containsAnyFold(task.Name, notContains) {
			return false
		}
	}
	return true
}

// now is replaced in tests so overrun checks do not depend on the wall clock.
var now = time.Now

var estimateRE = regexp.MustCompile(`Est: (?:(\d+)h)?(?:(\d+)m)?`)

// anyOverrun reports whether a running task has run longer than its estimate
// times ratio. Tasks without a start time or with a zero estimate never overrun,
// so a missing estimate cannot stop apps by itself.
// ponytail: start time is read in the machine's local timezone, not TaskChute's user timezone.
func anyOverrun(tasks []tcc2.RunningTask, ratio float64, at time.Time) bool {
	for _, task := range tasks {
		match := estimateRE.FindStringSubmatch(task.Meta)
		if match == nil || (match[1] == "" && match[2] == "") || task.Date == "" || task.Start == "" {
			continue
		}
		hours, _ := strconv.Atoi(match[1])
		minutes, _ := strconv.Atoi(match[2])
		estimate := time.Duration(hours*60+minutes) * time.Minute
		if estimate == 0 {
			continue
		}
		start, err := time.ParseInLocation("2006-01-02 15:04", task.Date+" "+task.Start, time.Local)
		if err != nil {
			continue
		}
		// Why: TaskChute files a task under its own day, which starts at "Start of Day"
		// (e.g. 19:00 the evening before), so Date+Start can land a day ahead of the real
		// start. A running task cannot start in the future, so step back a day.
		if start.After(at) {
			start = start.AddDate(0, 0, -1)
		}
		if at.Sub(start) > time.Duration(float64(estimate)*ratio) {
			return true
		}
	}
	return false
}
func containsAnyFold(value string, needles []string) bool {
	lower := strings.ToLower(value)
	for _, needle := range needles {
		if strings.Contains(lower, strings.ToLower(needle)) {
			return true
		}
	}
	return false
}
