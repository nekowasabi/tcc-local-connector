package idleguard

import (
	"testing"
	"time"
)

func testConfig() Config {
	return Config{Enabled: true, IdleMinutes: 15, GraceSeconds: 60, ExcludeApps: []string{"Finder"}, DryRun: false}
}

func idleInput(now time.Time, idle time.Duration, hasTask, known, paused bool) Input {
	return Input{Now: now, Idle: idle, HasRunningTask: hasTask, TaskKnown: known, Paused: paused}
}

func TestWatchingWarnsThenFiresAfterGrace(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := testConfig()
	idle := 16 * time.Minute
	effects := g.Tick(idleInput(now, idle, false, true, false), cfg)
	if len(effects) != 1 || effects[0].Kind != EffectWarn {
		t.Fatalf("warn effects = %#v", effects)
	}
	if g.PhaseOf() != PhaseCountdown {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
	if effects[0].Message != WarnMessage(cfg) || effects[0].Title != WarnTitle {
		t.Fatalf("warn text = %#v", effects[0])
	}
	before := g.Tick(idleInput(now.Add(59*time.Second), idle, false, true, false), cfg)
	if len(before) != 0 {
		t.Fatalf("pre-grace effects = %#v", before)
	}
	fire := g.Tick(idleInput(now.Add(61*time.Second), idle, false, true, false), cfg)
	if len(fire) != 1 || fire[0].Kind != EffectFire {
		t.Fatalf("fire effects = %#v", fire)
	}
	if fire[0].Title != StartTitle || fire[0].Message != StartMessage {
		t.Fatalf("fire text = %#v", fire[0])
	}
	if g.PhaseOf() != PhaseCooldown {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
}

func TestCooldownDoesNotRefire(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := Config{Enabled: true, IdleMinutes: 15, GraceSeconds: 0}
	idle := 16 * time.Minute
	effects := g.Tick(idleInput(now, idle, false, true, false), cfg)
	if len(effects) != 2 || effects[0].Kind != EffectWarn || effects[1].Kind != EffectFire {
		t.Fatalf("grace-0 effects = %#v", effects)
	}
	if g.PhaseOf() != PhaseCooldown {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
	for i := 0; i < 3; i++ {
		if more := g.Tick(idleInput(now.Add(time.Duration(i+1)*time.Minute), idle, false, true, false), cfg); len(more) != 0 {
			t.Fatalf("cooldown refired: %#v", more)
		}
	}
}

func TestRearmAfterActivityThenIdleAgain(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := Config{Enabled: true, IdleMinutes: 15, GraceSeconds: 0}
	idle := 16 * time.Minute
	if effects := g.Tick(idleInput(now, idle, false, true, false), cfg); len(effects) != 2 {
		t.Fatalf("first fire = %#v", effects)
	}
	if more := g.Tick(idleInput(now.Add(time.Minute), time.Minute, false, true, false), cfg); len(more) != 0 {
		t.Fatalf("activity tick = %#v", more)
	}
	if g.PhaseOf() != PhaseWatching {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
	if effects := g.Tick(idleInput(now.Add(2*time.Minute), idle, false, true, false), cfg); len(effects) != 2 {
		t.Fatalf("second fire = %#v", effects)
	}
}

func TestCountdownCancelsOnActivity(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := testConfig()
	idle := 16 * time.Minute
	if effects := g.Tick(idleInput(now, idle, false, true, false), cfg); len(effects) != 1 {
		t.Fatalf("warn = %#v", effects)
	}
	cancelled := g.Tick(idleInput(now.Add(10*time.Second), time.Minute, false, true, false), cfg)
	if len(cancelled) != 1 || cancelled[0].Kind != EffectCancel {
		t.Fatalf("cancel = %#v", cancelled)
	}
	if g.PhaseOf() != PhaseWatching {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
}

func TestCountdownCancelsOnTaskStart(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := testConfig()
	idle := 16 * time.Minute
	if effects := g.Tick(idleInput(now, idle, false, true, false), cfg); len(effects) != 1 {
		t.Fatalf("warn = %#v", effects)
	}
	cancelled := g.Tick(idleInput(now.Add(10*time.Second), idle, true, true, false), cfg)
	if len(cancelled) != 1 || cancelled[0].Kind != EffectCancel {
		t.Fatalf("cancel = %#v", cancelled)
	}
}

func TestNeverFiresWhenDisabledPausedOrUnknown(t *testing.T) {
	now := time.Now()
	idle := 60 * time.Minute
	cases := []struct {
		name string
		cfg  Config
		in   Input
	}{
		{"disabled", Config{Enabled: false, IdleMinutes: 15, GraceSeconds: 60}, idleInput(now, idle, false, true, false)},
		{"paused", testConfig(), idleInput(now, idle, false, true, true)},
		{"unknown", testConfig(), idleInput(now, idle, false, false, false)},
		{"running", testConfig(), idleInput(now, idle, true, true, false)},
		{"below", testConfig(), idleInput(now, 14*time.Minute, false, true, false)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			g := New()
			if effects := g.Tick(tc.in, tc.cfg); len(effects) != 0 {
				t.Fatalf("effects = %#v", effects)
			}
			if g.PhaseOf() != PhaseWatching {
				t.Fatalf("phase = %q", g.PhaseOf())
			}
		})
	}
}

func TestCountdownIgnoresWhenPausedOrUnknown(t *testing.T) {
	now := time.Now()
	cfg := testConfig()
	idle := 16 * time.Minute
	for _, tc := range []struct {
		name string
		in   Input
	}{
		{"paused", idleInput(now.Add(10*time.Second), idle, false, true, true)},
		{"unknown", idleInput(now.Add(10*time.Second), idle, false, false, false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := New()
			if effects := g.Tick(idleInput(now, idle, false, true, false), cfg); len(effects) != 1 {
				t.Fatalf("warn = %#v", effects)
			}
			cancelled := g.Tick(tc.in, cfg)
			if len(cancelled) != 1 || cancelled[0].Kind != EffectCancel {
				t.Fatalf("cancel = %#v", cancelled)
			}
		})
	}
}

func TestCooldownRearmsOnTaskStart(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := Config{Enabled: true, IdleMinutes: 15, GraceSeconds: 0}
	idle := 16 * time.Minute
	if effects := g.Tick(idleInput(now, idle, false, true, false), cfg); len(effects) != 2 {
		t.Fatalf("fire = %#v", effects)
	}
	if more := g.Tick(idleInput(now.Add(time.Minute), idle, true, true, false), cfg); len(more) != 0 {
		t.Fatalf("task tick = %#v", more)
	}
	if g.PhaseOf() != PhaseWatching {
		t.Fatalf("phase = %q", g.PhaseOf())
	}
}

func TestDryRunFlagFlowsToFireEffect(t *testing.T) {
	now := time.Now()
	g := New()
	cfg := Config{Enabled: true, IdleMinutes: 15, GraceSeconds: 0, DryRun: true}
	effects := g.Tick(idleInput(now, 16*time.Minute, false, true, false), cfg)
	if len(effects) != 2 || !effects[1].DryRun {
		t.Fatalf("effects = %#v", effects)
	}
}

func TestParseHIDIdleTime(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   time.Duration
		wantOK bool
	}{
		{"decimal", "\"HIDIdleTime\" = 1234567890", 1234567890 * time.Nanosecond, true},
		{"unquoted", "HIDIdleTime = 42", 42 * time.Nanosecond, true},
		{"hex", "\"HIDIdleTime\" = 0x10", 16 * time.Nanosecond, true},
		{"whitespace", "  \"HIDIdleTime\"   =   999  ", 999 * time.Nanosecond, true},
		{"multiline", "{\n  \"HIDIdleTime\" = 7\n}", 7 * time.Nanosecond, true},
		{"missing", "\"OtherKey\" = 5", 0, false},
		{"empty", "", 0, false},
		{"garbage", "\"HIDIdleTime\" = abc", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ParseHIDIdleTime([]byte(tc.text))
			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("ParseHIDIdleTime(%q) = (%v, %v), want (%v, %v)", tc.text, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestMatchExcludedCaseInsensitive(t *testing.T) {
	excludes := []string{"Finder", "com.apple.dock"}
	for _, tc := range []struct {
		name, bundle string
		want         bool
	}{
		{"Finder", "", true},
		{"finder", "", true},
		{"Safari", "com.apple.dock", true},
		{"Safari", "com.apple.Safari", false},
		{"", "", false},
	} {
		if got := MatchExcluded(tc.name, tc.bundle, excludes); got != tc.want {
			t.Fatalf("MatchExcluded(%q, %q) = %v, want %v", tc.name, tc.bundle, got, tc.want)
		}
	}
}

func TestFilterAppsSkipsExcludedAndEmpty(t *testing.T) {
	apps := []App{
		{Name: "Finder"},
		{Name: "Safari", BundleID: "com.apple.Safari"},
		{Name: ""},
		{Name: "missing value"},
	}
	filtered := FilterApps(apps, []string{"Finder"})
	if len(filtered) != 1 || filtered[0].Name != "Safari" {
		t.Fatalf("filtered = %#v", filtered)
	}
}

func TestParseForegroundApps(t *testing.T) {
	output := `{{"Finder", "com.apple.finder", 381}, {"Safari", "com.apple.Safari", 512}, {"NoBundle", "missing value", 700}}`
	apps := ParseForegroundApps(output)
	if len(apps) != 3 {
		t.Fatalf("apps = %#v", apps)
	}
	if apps[0].Name != "Finder" || apps[0].BundleID != "com.apple.finder" {
		t.Fatalf("apps[0] = %#v", apps[0])
	}
	if apps[2].Name != "NoBundle" || apps[2].BundleID != "" {
		t.Fatalf("apps[2] = %#v", apps[2])
	}
	if got := ParseForegroundApps(""); len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestEscapeAppleScriptString(t *testing.T) {
	if got := EscapeAppleScriptString(`a"b\c`); got != `a\"b\\c` {
		t.Fatalf("escaped = %q", got)
	}
}
