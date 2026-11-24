package rules

import (
	"testing"
	"time"

	"github.com/takets/tcc-local-connector/internal/config"
	"github.com/takets/tcc-local-connector/internal/tcc2"
)

func TestEvaluate_OverrunRatio(t *testing.T) {
	defer func(original func() time.Time) { now = original }(now)
	cfg := &config.Config{Rules: []config.Rule{{ID: "overrun", Match: config.Match{OverrunRatio: 2}}}}
	task := tcc2.RunningTask{Name: "ひつじ枕を洗う", Date: "2026-11-22", Start: "16:33", Meta: "16:33, Est: 02m"}
	start := time.Date(2026, 11, 22, 16, 33, 0, 0, time.Local)
	cases := []struct {
		name    string
		elapsed time.Duration
		task    tcc2.RunningTask
		want    bool
	}{
		{"within ratio", 4 * time.Minute, task, false},
		{"over ratio", 4*time.Minute + time.Second, task, true},
		{"hours estimate", 2 * time.Hour, tcc2.RunningTask{Date: "2026-11-22", Start: "16:33", Meta: "16:33, Est: 1h05m"}, false},
		{"zero estimate never overruns", time.Hour, tcc2.RunningTask{Date: "2026-11-22", Start: "16:33", Meta: "16:33, Est: 00m"}, false},
		{"no estimate never overruns", time.Hour, tcc2.RunningTask{Date: "2026-11-22", Start: "16:33", Meta: "16:33"}, false},
		{"hours-only estimate", 2*time.Hour + time.Second, tcc2.RunningTask{Date: "2026-11-22", Start: "16:33", Meta: "16:33, Est: 01h"}, true},
		// With Start of Day -05:00, TaskChute files an evening start under the next day's date.
		{"next-day date from Start of Day", time.Hour + time.Second, tcc2.RunningTask{Date: "2026-11-23", Start: "16:33", Meta: "16:33, Est: 30m"}, true},
	}
	for _, c := range cases {
		now = func() time.Time { return start.Add(c.elapsed) }
		if got := Evaluate(cfg, []tcc2.RunningTask{c.task}).ActiveRuleIDs["overrun"]; got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	now = func() time.Time { return start.Add(time.Hour) }
	if Evaluate(cfg, nil).ActiveRuleIDs["overrun"] {
		t.Errorf("no running task must not overrun")
	}
}

// The SNS overrun rule must fire on the SNS task's own overrun, not on another running task's.
func TestEvaluate_OverrunScopedToNameMatch(t *testing.T) {
	defer func(original func() time.Time) { now = original }(now)
	cfg := &config.Config{Rules: []config.Rule{{ID: "sns-overrun", Match: config.Match{TaskNameContains: []string{"SNS /"}, OverrunRatio: 1}}}}
	sns := tcc2.RunningTask{Name: "sns / slack", Date: "2026-11-25", Start: "19:12", Meta: "19:12, Est: 30m"}
	floss := tcc2.RunningTask{Name: "フロスをする", Date: "2026-11-25", Start: "19:12", Meta: "19:12, Est: 05m"}
	start := time.Date(2026, 11, 24, 19, 12, 0, 0, time.Local)
	cases := []struct {
		name    string
		elapsed time.Duration
		tasks   []tcc2.RunningTask
		want    bool
	}{
		{"other task overruns, sns within estimate", 10 * time.Minute, []tcc2.RunningTask{sns, floss}, false},
		{"sns overruns", 30*time.Minute + time.Second, []tcc2.RunningTask{sns, floss}, true},
		{"no sns task", time.Hour, []tcc2.RunningTask{floss}, false},
	}
	for _, c := range cases {
		now = func() time.Time { return start.Add(c.elapsed) }
		if got := Evaluate(cfg, c.tasks).ActiveRuleIDs["sns-overrun"]; got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
