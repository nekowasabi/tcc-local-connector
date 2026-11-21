//go:build darwin

package idleguard

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// MAC_UNVERIFIED: the adapters below shell out to ioreg and osascript and
// can only be verified on a real Mac. Quitting is always a normal
// AppleScript `quit`; the adapters never terminate processes.

// listScript lists regular foreground processes as TSV (name TAB bundle id).
// MAC_UNVERIFIED: System Events `get {name, bundle identifier} of every process`
// returns parallel lists, not per-app triples, so the script emits TSV instead.
const listScript = `set output to ""
tell application "System Events"
  repeat with p in (every application process whose background only is false)
    set n to name of p
    set b to ""
    try
      set b to bundle identifier of p
    end try
    set output to output & n & tab & b & linefeed
  end repeat
end tell
return output`

// SystemIdleDuration reports how long the Mac has seen no user input.
// MAC_UNVERIFIED: reads HIDIdleTime from ioreg on darwin.
func SystemIdleDuration() (time.Duration, bool) {
	output, err := exec.Command("ioreg", "-c", "IOHIDSystem").Output()
	if err != nil {
		return 0, false
	}
	return ParseHIDIdleTime(output)
}

// ListForegroundApps returns regular foreground application processes.
// MAC_UNVERIFIED: parses osascript System Events output on darwin.
func ListForegroundApps() ([]App, error) {
	output, err := exec.Command("osascript", "-e", listScript).Output()
	if err != nil {
		return nil, err
	}
	return ParseForegroundApps(string(output)), nil
}

// QuitApp asks one application to quit normally, falling back to the bundle
// identifier when the name-based quit fails. It never force-quits.
// MAC_UNVERIFIED: osascript quit on darwin.
func QuitApp(app App) error {
	nameScript := fmt.Sprintf(`tell application %s to quit`, quoteApple(app.Name))
	if err := exec.Command("osascript", "-e", nameScript).Run(); err == nil {
		return nil
	} else if app.BundleID == "" {
		return err
	}
	idScript := fmt.Sprintf(`tell application id %s to quit`, quoteApple(app.BundleID))
	if idErr := exec.Command("osascript", "-e", idScript).Run(); idErr != nil {
		return idErr
	}
	return nil
}

func quoteApple(value string) string {
	return `"` + EscapeAppleScriptString(value) + `"`
}

// taskChuteAppNames are tried in order when bringing TaskChute forward.
var taskChuteAppNames = []string{"TaskChute Cloud 2", "TaskChute Cloud", "TaskChute"}

// ActivateTaskChute brings TaskChute forward. The first successful activation
// wins.
// MAC_UNVERIFIED: osascript activate on darwin.
func ActivateTaskChute() error {
	var lastErr error
	for _, name := range taskChuteAppNames {
		script := fmt.Sprintf(`tell application %s to activate`, quoteApple(name))
		if err := exec.Command("osascript", "-e", script).Run(); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no TaskChute application found")
	}
	return lastErr
}

// NotifyUser shows a macOS banner via osascript display notification.
// MAC_UNVERIFIED: osascript display notification on darwin.
func NotifyUser(title, message string) error {
	script := fmt.Sprintf(`display notification %s with title %s`, quoteApple(message), quoteApple(title))
	return exec.Command("osascript", "-e", script).Run()
}
