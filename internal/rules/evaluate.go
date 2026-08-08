package rules

import (
	"strings"

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
	if len(contains) == 0 && len(notContains) == 0 {
		return true
	}
	if len(contains) > 0 {
		matched := false
		for _, task := range tasks {
			if containsAnyFold(task.Name, contains) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	for _, task := range tasks {
		if containsAnyFold(task.Name, notContains) {
			return false
		}
	}
	return true
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
