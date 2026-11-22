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
