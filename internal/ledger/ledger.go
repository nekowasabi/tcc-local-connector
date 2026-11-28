package ledger

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/takets/tcc-local-connector/internal/constants"
	"github.com/takets/tcc-local-connector/internal/state"
)

type Entry struct {
	ProcessID string `json:"process_id"`
	PID       int    `json:"pid"`
	PSLstart  string `json:"ps_lstart"`
	PSArgs    string `json:"ps_args"`
}
type Ledger struct {
	Version int              `json:"version"`
	Entries map[string]Entry `json:"entries"`
	path    string
	mu      sync.Mutex
}

func Load(path string) (*Ledger, error) {
	ledger := &Ledger{Version: constants.LedgerVersion, Entries: map[string]Entry{}, path: path}
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ledger, nil
		}
		return nil, err
	}
	if json.Unmarshal(body, ledger) != nil || ledger.Version != constants.LedgerVersion {
		return &Ledger{Version: constants.LedgerVersion, Entries: map[string]Entry{}, path: path}, nil
	}
	if ledger.Entries == nil {
		ledger.Entries = map[string]Entry{}
	}
	return ledger, nil
}
func (l *Ledger) Save() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	body, err := json.Marshal(l)
	if err != nil {
		return err
	}
	return state.WriteFile(l.path, body)
}
func (l *Ledger) Put(entry Entry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.Entries[entry.ProcessID] = entry
}
func (l *Ledger) Remove(id string) { l.mu.Lock(); defer l.mu.Unlock(); delete(l.Entries, id) }
func (l *Ledger) Get(id string) (Entry, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	value, ok := l.Entries[id]
	return value, ok
}
