//go:build !darwin

package idleguard

import "time"

// SystemIdleDuration is a no-op outside darwin: idle auto-quit is Mac-only.
func SystemIdleDuration() (time.Duration, bool) {
	return 0, false
}

// ListForegroundApps is a no-op outside darwin.
func ListForegroundApps() ([]App, error) {
	return nil, nil
}

// QuitApp is a no-op outside darwin.
func QuitApp(App) error {
	return nil
}

// ActivateTaskChute is a no-op outside darwin.
func ActivateTaskChute() error {
	return nil
}

// NotifyUser is a no-op outside darwin.
func NotifyUser(string, string) error {
	return nil
}
