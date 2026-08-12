package browserpolicy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/takets/tcc-local-connector/internal/constants"
)

type Store struct {
	mu         sync.Mutex
	path       string
	generation uint64
	now        func() time.Time
	write      func(string, []byte) error
}

type committedWriteError struct{ err error }

func (e committedWriteError) Error() string { return e.err.Error() }
func (e committedWriteError) Unwrap() error { return e.err }

func NewStore(path string) *Store {
	store := &Store{path: path, now: time.Now, write: writeAtomic}
	if payload, err := os.ReadFile(path); err == nil {
		if policy, err := Decode(payload); err == nil {
			store.generation = policy.Generation
		}
	}
	return store
}

func (s *Store) Path() string { return s.path }

func (s *Store) Publish(enforce, dryRun bool, domains, plannedDomains []string) (Policy, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	domains, err := NormalizeDomains(domains)
	if err != nil {
		return Policy{}, err
	}
	plannedDomains, err = NormalizeDomains(plannedDomains)
	if err != nil {
		return Policy{}, err
	}
	policy := Policy{
		Version:        constants.BrowserPolicyVersion,
		Generation:     s.generation + 1,
		Enforce:        enforce,
		DryRun:         dryRun,
		Domains:        domains,
		PlannedDomains: plannedDomains,
		UpdatedAt:      s.now().UTC().Format(time.RFC3339Nano),
	}
	if err := Validate(policy); err != nil {
		return Policy{}, err
	}
	payload, err := json.Marshal(policy)
	if err != nil {
		return Policy{}, err
	}
	payload = append(payload, '\n')
	if err := s.write(s.path, payload); err != nil {
		var committedErr committedWriteError
		if errors.As(err, &committedErr) {
			// Why: Once rename made this generation visible, reusing it would
			// conflict with the Host's same-generation content-change rejection.
			s.generation = policy.Generation
		}
		return Policy{}, err
	}
	s.generation = policy.Generation
	return policy, nil
}

func (s *Store) PublishEmpty() (Policy, error) {
	return s.Publish(false, false, []string{}, []string{})
}

func writeAtomic(path string, payload []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".browser-policy-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	committed := false
	defer func() {
		_ = temp.Close()
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temp.Write(payload); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	committed = true
	directory, err := os.Open(dir)
	if err != nil {
		return committedWriteError{err: err}
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return committedWriteError{err: fmt.Errorf("sync browser policy directory: %w", err)}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		if err != nil {
			return committedWriteError{err: err}
		}
		return committedWriteError{err: errors.New("browser policy file has unexpected permissions")}
	}
	return nil
}
