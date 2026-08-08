package state

import (
	"sync"
	"time"
)

type State string

const (
	StateStarting    State = "starting"
	StateFetching    State = "fetching"
	StateActive      State = "active"
	StateDegraded    State = "degraded"
	StateReleased    State = "released"
	StatePaused      State = "paused"
	StateConfigError State = "config_error"
)

type Transition struct {
	State           State  `json:"state"`
	Code            string `json:"code,omitempty"`
	ReleaseControls bool   `json:"release_controls"`
	Reevaluate      bool   `json:"reevaluate"`
}
type Machine struct {
	mu           sync.RWMutex
	state        State
	failures     int
	lastSuccess  time.Time
	firstFailure time.Time
}

func NewMachine() *Machine        { return &Machine{state: StateStarting} }
func (m *Machine) Current() State { m.mu.RLock(); defer m.mu.RUnlock(); return m.state }
func (m *Machine) Transition(event string, now time.Time, grace time.Duration) Transition {
	m.mu.Lock()
	defer m.mu.Unlock()
	next := Transition{State: m.state}
	switch event {
	case "config_error":
		if m.state == StateStarting {
			m.state = StateConfigError
		}
	case "start":
		if m.state == StateStarting {
			m.state = StateFetching
		}
	case "success":
		code := ""
		if m.state == StateDegraded {
			code = "recovered"
		}
		m.state = StateActive
		m.failures = 0
		m.lastSuccess = now
		m.firstFailure = time.Time{}
		next.Code = code
	case "failure":
		if m.state != StatePaused && m.state != StateConfigError {
			m.state = StateDegraded
			m.failures++
			if m.firstFailure.IsZero() {
				m.firstFailure = now
			}
		}
	case "grace_expired":
		if m.state == StateDegraded && !m.firstFailure.IsZero() && now.After(m.firstFailure.Add(grace)) {
			m.state = StateReleased
			next.Code = "grace_expired"
			next.ReleaseControls = true
		}
	case "pause":
		if m.state != StateConfigError {
			m.state = StatePaused
			next.Code = "paused"
			next.ReleaseControls = true
		}
	case "resume":
		if m.state == StatePaused {
			m.state = StateFetching
			next.Code = "resumed"
			next.Reevaluate = true
		}
	case "reload_ok":
		if m.state == StateConfigError {
			m.state = StateFetching
			next.Reevaluate = true
		}
	case "wake":
		next.Reevaluate = true
	}
	next.State = m.state
	return next
}
