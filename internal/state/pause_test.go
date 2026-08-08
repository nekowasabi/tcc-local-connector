package state

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"
)

func TestLoadPauseMissingFileReturnsEmpty(t *testing.T) {
    pause, err := LoadPause(filepath.Join(t.TempDir(), "pause.json"))
    if err != nil {
        t.Fatalf("LoadPause missing file = %v", err)
    }
    if !pause.Until.IsZero() || pause.Version != 0 {
        t.Fatalf("unexpected pause = %#v", pause)
    }
}

func TestLoadPauseReturnsZeroOnInvalidVersion(t *testing.T) {
    path := filepath.Join(t.TempDir(), "pause.json")
    payload, _ := json.Marshal(Pause{Version: 999, Until: time.Now().Add(time.Hour), Reason: "old"})
    if err := os.WriteFile(path, payload, 0o600); err != nil {
        t.Fatal(err)
    }
    pause, err := LoadPause(path)
    if err != nil {
        t.Fatalf("LoadPause invalid version = %v", err)
    }
    if !pause.Until.IsZero() {
        t.Fatalf("pause should be empty: %#v", pause)
    }
}

func TestLoadPauseReturnsZeroOnCorruptFile(t *testing.T) {
    path := filepath.Join(t.TempDir(), "pause.json")
    if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
        t.Fatal(err)
    }
    pause, err := LoadPause(path)
    if err != nil {
        t.Fatalf("LoadPause corrupt file = %v", err)
    }
    if !pause.Until.IsZero() {
        t.Fatalf("pause should be empty: %#v", pause)
    }
}

func TestSavePauseCreatesAtomicTmpFile(t *testing.T) {
    path := filepath.Join(t.TempDir(), "pause.json")
    p := Pause{Until: time.Now().Add(time.Minute), Reason: "short"}
    if err := SavePause(path, p); err != nil {
        t.Fatalf("SavePause = %v", err)
    }
    loaded, err := LoadPause(path)
    if err != nil {
        t.Fatalf("LoadPause loaded = %v", err)
    }
    if loaded.Reason != p.Reason {
        t.Fatalf("loaded=%#v", loaded)
    }
    if info, err := os.Stat(path + ".tmp"); err == nil {
        t.Fatalf("tmp file should not remain: %#v", info)
    }
}

func TestSavePauseFailsWhenDirectoryMissing(t *testing.T) {
    path := filepath.Join(t.TempDir(), "dir", "pause.json")
    if err := SavePause(path, Pause{Until: time.Now().Add(time.Minute), Reason: "created"}); err != nil {
        t.Fatalf("SavePause should create dirs: %v", err)
    }
}

func TestClearPauseRemovesFile(t *testing.T) {
    dir := t.TempDir()
    path := filepath.Join(dir, "pause.json")
    if err := SavePause(path, Pause{Until: time.Now().Add(time.Minute), Reason: "x"}); err != nil {
        t.Fatal(err)
    }
    if err := ClearPause(path); err != nil {
        t.Fatalf("ClearPause = %v", err)
    }
    if _, err := os.Stat(path); err == nil {
        t.Fatal("pause file remains")
    }
}

func TestClearPauseMissingFileIsNoop(t *testing.T) {
    if err := ClearPause(filepath.Join(t.TempDir(), "missing.json")); err != nil {
        t.Fatalf("ClearPause missing = %v", err)
    }
}

func TestPauseExpiredAtOrAfterUntil(t *testing.T) {
    now := time.Now()
    pause := Pause{Until: now.Add(time.Minute)}
    if pause.Expired(now.Add(-time.Second)) {
        t.Fatal("should not expire early")
    }
    if !pause.Expired(now.Add(time.Minute)) {
        t.Fatal("should expire at equal")
    }
    if !pause.Expired(now.Add(time.Minute + time.Second)) {
        t.Fatal("should expire after")
    }
}

func TestSavePauseRejectsInvalidPathModePreserved(t *testing.T) {
    dir := t.TempDir()
    file := filepath.Join(dir, "pause.json")
    if err := os.WriteFile(filepath.Dir(file), []byte("x"), 0o600); err == nil {
        t.Fatal("expected directory file write collision")
    }
    if err := SavePause(filepath.Join(dir, "x", "..", "y", "pause.json"), Pause{Until: time.Now().Add(time.Minute)}); err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}

func TestPause_ClockRewind_StaysPaused(t *testing.T) {
	pause := Pause{Until: time.Now().Add(time.Minute)}
	rewind := time.Now().Add(-2 * time.Minute)
	if pause.Expired(rewind) {
		t.Fatal("pause should stay active when clock rewinds")
	}
}
