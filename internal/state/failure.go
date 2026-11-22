package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

const (
	FailureFileName = "failure.json"
	failureVersion  = 1
)

type failureState struct {
	Version      int       `json:"version"`
	FirstFailure time.Time `json:"first_failure"`
}

func LoadFailure(path string) (time.Time, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	var value failureState
	if err := json.Unmarshal(body, &value); err != nil {
		return time.Time{}, err
	}
	if value.Version != failureVersion || value.FirstFailure.IsZero() {
		return time.Time{}, errors.New("invalid failure state")
	}
	return value.FirstFailure, nil
}

func SaveFailure(path string, firstFailure time.Time) error {
	body, err := json.Marshal(failureState{Version: failureVersion, FirstFailure: firstFailure.UTC()})
	if err != nil {
		return err
	}
	return WriteFile(path, body)
}

func ClearFailure(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// WriteFile atomically replaces a private state file.
func WriteFile(path string, body []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
