// Package idleguard quits regular foreground apps when TaskChute has no
// running task and the Mac has seen no user input for a configured time.
//
// The core is a pure state machine (Guard.Tick) with no I/O so it is fully
// testable on any platform. Platform adapters (idle time, app listing,
// quitting, notification, activation) live behind build tags; outside darwin
// they are no-ops.
package idleguard

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Phase is the guard state.
type Phase string

const (
	// PhaseWatching waits for the idle-without-task condition.
	PhaseWatching Phase = "watching"
	// PhaseCountdown waits out the grace period after warning.
	PhaseCountdown Phase = "countdown"
	// PhaseCooldown suppresses re-firing until activity or a task returns.
	PhaseCooldown Phase = "cooldown"
)

// EffectKind identifies a side effect the caller should perform.
type EffectKind string

const (
	// EffectWarn asks the caller to show the pre-quit warning.
	EffectWarn EffectKind = "warn"
	// EffectCancel asks the caller to log the cancelled countdown.
	EffectCancel EffectKind = "cancel"
	// EffectFire asks the caller to quit apps, activate TaskChute (unless
	// dry-run), and show the start-next-task notification.
	EffectFire EffectKind = "fire"
)

// Config mirrors the idle_guard file settings plus the dry-run flag.
type Config struct {
	Enabled      bool
	IdleMinutes  int
	GraceSeconds int
	ExcludeApps  []string
	DryRun       bool
}

// Input is one guard tick.
type Input struct {
	Now            time.Time
	Idle           time.Duration
	HasRunningTask bool
	TaskKnown      bool
	Paused         bool
}

// Effect is one requested side effect.
type Effect struct {
	Kind    EffectKind
	Title   string
	Message string
	DryRun  bool
}

// WarnTitle is the warning notification title.
const WarnTitle = "タスク未開始・無操作"

// WarnMessage formats the warning body from the configured thresholds.
func WarnMessage(cfg Config) string {
	return fmt.Sprintf("%d分タスク未開始・無操作です。%d秒後にアプリを終了します", cfg.IdleMinutes, cfg.GraceSeconds)
}

// StartTitle is the post-quit notification title.
const StartTitle = "タスク未開始"

// StartMessage is the post-quit notification body.
const StartMessage = "次のタスクを開始"

// Guard is the idle-quit state machine. The zero value is a watching guard.
type Guard struct {
	mu       sync.Mutex
	phase    Phase
	warnedAt time.Time
}

// New returns a guard in the watching phase.
func New() *Guard {
	return &Guard{phase: PhaseWatching}
}

// Reset returns the machine to watching. Used when the feature is disabled.
func (g *Guard) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.phase = PhaseWatching
	g.warnedAt = time.Time{}
}

// PhaseOf reports the current phase.
func (g *Guard) PhaseOf() Phase {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase == "" {
		return PhaseWatching
	}
	return g.phase
}

// Tick advances the machine one step and returns the requested effects.
func (g *Guard) Tick(in Input, cfg Config) []Effect {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.phase == "" {
		g.phase = PhaseWatching
	}
	threshold := time.Duration(cfg.IdleMinutes) * time.Minute
	idleEnough := in.Idle >= threshold
	quiet := cfg.Enabled && in.TaskKnown && !in.Paused && !in.HasRunningTask && idleEnough
	switch g.phase {
	case PhaseWatching:
		if !quiet {
			return nil
		}
		g.phase = PhaseCountdown
		g.warnedAt = in.Now
		effects := []Effect{{Kind: EffectWarn, Title: WarnTitle, Message: WarnMessage(cfg)}}
		if cfg.GraceSeconds <= 0 {
			g.phase = PhaseCooldown
			effects = append(effects, Effect{Kind: EffectFire, Title: StartTitle, Message: StartMessage, DryRun: cfg.DryRun})
		}
		return effects
	case PhaseCountdown:
		if !quiet {
			g.phase = PhaseWatching
			return []Effect{{Kind: EffectCancel}}
		}
		if in.Now.Sub(g.warnedAt) >= time.Duration(cfg.GraceSeconds)*time.Second {
			g.phase = PhaseCooldown
			return []Effect{{Kind: EffectFire, Title: StartTitle, Message: StartMessage, DryRun: cfg.DryRun}}
		}
		return nil
	case PhaseCooldown:
		if in.HasRunningTask || !idleEnough {
			g.phase = PhaseWatching
		}
		return nil
	default:
		g.phase = PhaseWatching
		return nil
	}
}

// App is one foreground application process.
type App struct {
	Name     string
	BundleID string
}

// MatchExcluded reports whether name or bundleID matches the exclusion list
// with a case-insensitive exact match.
func MatchExcluded(name, bundleID string, excludes []string) bool {
	for _, excluded := range excludes {
		if strings.EqualFold(strings.TrimSpace(excluded), strings.TrimSpace(name)) {
			return true
		}
		if bundleID != "" && strings.EqualFold(strings.TrimSpace(excluded), strings.TrimSpace(bundleID)) {
			return true
		}
	}
	return false
}

// FilterApps drops excluded apps from the quit candidates.
func FilterApps(apps []App, excludes []string) []App {
	kept := make([]App, 0, len(apps))
	for _, app := range apps {
		if app.Name == "" || app.Name == "missing value" {
			continue
		}
		if MatchExcluded(app.Name, app.BundleID, excludes) {
			continue
		}
		kept = append(kept, app)
	}
	return kept
}

var hidIdlePattern = regexp.MustCompile(`"?HIDIdleTime"?\s*=\s*(0[xX][0-9a-fA-F]+|[0-9]+)`)

// ParseHIDIdleTime extracts the HIDIdleTime nanosecond counter from ioreg
// output. It accepts decimal and hexadecimal values with optional quotes and
// surrounding whitespace. ok is false when the key is missing or unparsable.
func ParseHIDIdleTime(output []byte) (idle time.Duration, ok bool) {
	match := hidIdlePattern.FindSubmatch(output)
	if match == nil {
		return 0, false
	}
	value, err := strconv.ParseUint(string(match[1]), 0, 64)
	if err != nil {
		return 0, false
	}
	const maxInt64 = uint64(1<<63 - 1)
	if value > maxInt64 {
		value = maxInt64
	}
	return time.Duration(value), true
}

// ParseForegroundApps parses darwin TSV (`name<TAB>bundle`) or an AppleScript
// list of per-app triples `{{"Name", "bundle.id", pid}, ...}`.
func ParseForegroundApps(output string) []App {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return nil
	}
	if strings.Contains(trimmed, "\t") {
		return parseForegroundTSV(trimmed)
	}
	var apps []App
	for _, group := range splitTopGroups(trimmed) {
		fields := splitTriple(group)
		if len(fields) != 3 {
			continue
		}
		name := unquoteApple(fields[0])
		bundleID := unquoteApple(fields[1])
		if bundleID == "missing value" {
			bundleID = ""
		}
		if name == "" || name == "missing value" {
			continue
		}
		apps = append(apps, App{Name: name, BundleID: bundleID})
	}
	return apps
}

func parseForegroundTSV(output string) []App {
	var apps []App
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		name, bundleID, _ := strings.Cut(line, "\t")
		name = strings.TrimSpace(name)
		bundleID = strings.TrimSpace(bundleID)
		if name == "" || name == "missing value" {
			continue
		}
		if bundleID == "missing value" {
			bundleID = ""
		}
		apps = append(apps, App{Name: name, BundleID: bundleID})
	}
	return apps
}

func splitTopGroups(output string) []string {
	var groups []string
	depth := 0
	start := -1
	for i, r := range output {
		switch r {
		case '{':
			if depth == 1 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 1 && start >= 0 {
				groups = append(groups, output[start+1:i])
				start = -1
			}
		}
	}
	if len(groups) == 0 {
		flat := strings.Trim(output, " {}")
		parts := splitTriple(flat)
		for i := 0; i+2 < len(parts)+1 && i+2 <= len(parts); i += 3 {
			groups = append(groups, strings.Join(parts[i:i+3], ","))
		}
	}
	return groups
}

func splitTriple(group string) []string {
	var fields []string
	var current bytes.Buffer
	inQuotes := false
	escaped := false
	for _, r := range group {
		switch {
		case escaped:
			current.WriteRune(r)
			escaped = false
		case r == '\\' && inQuotes:
			escaped = true
			current.WriteRune(r)
		case r == '"':
			inQuotes = !inQuotes
			current.WriteRune(r)
		case r == ',' && !inQuotes:
			fields = append(fields, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	fields = append(fields, current.String())
	return fields
}

func unquoteApple(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
		inner := value[1 : len(value)-1]
		replacer := strings.NewReplacer(`\"`, `"`, `\\`, `\`)
		return replacer.Replace(inner)
	}
	return value
}

// EscapeAppleScriptString escapes a value for embedding in a double-quoted
// AppleScript string.
func EscapeAppleScriptString(value string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return replacer.Replace(value)
}
