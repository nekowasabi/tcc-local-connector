package tcc2

import (
	"testing"
	"time"
)

// 19:12 JST with Start of Day -05:00 is already tomorrow's TaskChute date.
func TestFetchRangeIncludesTomorrow(t *testing.T) {
	now := time.Date(2026, 11, 24, 19, 12, 0, 0, time.FixedZone("JST", 9*3600))
	start, end := fetchRange(now)
	if start != "2026-11-23" || end != "2026-11-25" {
		t.Fatalf("got %s..%s, want 2026-11-23..2026-11-25", start, end)
	}
}
