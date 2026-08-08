package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type Pause struct {
	Version   int       `json:"version"`
	Until     time.Time `json:"until"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

func (p Pause) Expired(now time.Time) bool { return now.Equal(p.Until) || now.After(p.Until) }
func LoadPause(path string) (Pause, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Pause{}, nil
		}
		return Pause{}, err
	}
	var pause Pause
	if json.Unmarshal(body, &pause) != nil || pause.Version != constants.PauseStateVersion {
		_ = os.Remove(path)
		return Pause{}, nil
	}
	return pause, nil
}
func SavePause(path string, pause Pause) error {
	pause.Version = constants.PauseStateVersion
	if pause.CreatedAt.IsZero() {
		pause.CreatedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body, err := json.Marshal(pause)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err = os.WriteFile(tmp, body, constants.StateFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func ClearPause(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
