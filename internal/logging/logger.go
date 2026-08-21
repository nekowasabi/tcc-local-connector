// Package logging writes redacted, newline-delimited JSON diagnostics.
package logging

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

var emailPattern = regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`)

type Entry struct {
	Timestamp  time.Time `json:"ts"`
	Level      string    `json:"level"`
	Component  string    `json:"component"`
	Event      string    `json:"event"`
	CycleID    int64     `json:"cycle_id,omitempty"`
	ActionID   string    `json:"action_id,omitempty"`
	Phase      string    `json:"phase,omitempty"`
	Status     string    `json:"status,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	RuleID     string    `json:"rule_id,omitempty"`
	ActionType string    `json:"action_type,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
	ErrorCode  string    `json:"error_code,omitempty"`
	Message    string    `json:"msg,omitempty"`
}

type Logger struct {
	mu        sync.Mutex
	writer    io.Writer
	threshold int
}

func New(writer io.Writer, level string) *Logger {
	if writer == nil {
		writer = io.Discard
	}
	return &Logger{writer: writer, threshold: levelValue(level)}
}

func (l *Logger) Log(entry Entry) {
	if l == nil || levelValue(entry.Level) < l.threshold {
		return
	}
	entry.Message = redactSecrets(entry.Message)
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	body, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.writer.Write(append(body, '\n'))
}

// Rotate removes only connector log files older than the requested retention period.
func (l *Logger) Rotate(dir string, retainDays int, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	cutoff := now.AddDate(0, 0, -retainDays)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), constants.LogFileName) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, entry.Name()))
	}
}

func levelValue(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	default:
		return levelValue(constants.DefaultLogLevel)
	}
}

func redactSecrets(value string) string {
	value = emailPattern.ReplaceAllString(value, "[REDACTED]")
	if index := strings.Index(value, constants.LogRedactBearerPrefix); index >= 0 {
		value = value[:index] + "[REDACTED]"
	}
	if index := strings.Index(value, constants.LogRedactTokenExpires); index >= 0 {
		value = value[:index] + "[REDACTED]"
	}
	return value
}
