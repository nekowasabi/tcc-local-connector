package state

import (
	"encoding/json"
	"errors"
	"os"
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
	body, err := json.Marshal(pause)
	if err != nil {
		return err
	}
	return WriteFile(path, body)
}
func ClearPause(path string) error {
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

const PauseBudgetFileName = "pause-budget.json"

type pauseBudget struct {
	Version int    `json:"version"`
	LastDay string `json:"last_pause_day"`
}

func LoadPauseDay(path string) (string, error) {
	body, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var budget pauseBudget
	if err := json.Unmarshal(body, &budget); err != nil {
		return "", err
	}
	if _, err := time.Parse(time.DateOnly, budget.LastDay); err != nil || budget.Version != constants.PauseStateVersion {
		return "", errors.New("invalid pause budget")
	}
	return budget.LastDay, nil
}

func SavePauseDay(path, day string) error {
	if day == "" {
		return ClearPause(path)
	}
	body, err := json.Marshal(pauseBudget{Version: constants.PauseStateVersion, LastDay: day})
	if err != nil {
		return err
	}
	return WriteFile(path, body)
}
