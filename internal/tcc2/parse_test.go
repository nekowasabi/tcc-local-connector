package tcc2

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse_GoldenFixture(t *testing.T) {
	fixture := filepath.Join("testdata", "golden-taskchute.txt")
	text, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	result, err := ParseTaskChuteText(string(text), false)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	want := map[string]int{"Done": 111, "In Progress": 1, "Todo": 4}
	for key, value := range want {
		if result.StatusCounts[key] != value {
			t.Fatalf("status_counts[%s]=%d, want=%d", key, result.StatusCounts[key], value)
		}
	}
	if len(result.RunningTasks) != 1 {
		t.Fatalf("running task count=%d, want=1", len(result.RunningTasks))
	}
}
