package state

import (
	"testing"
	"time"
)

func TestMachine_StartingToConfigError(t *testing.T) {
	machine := NewMachine()
	transition := machine.Transition("config_error", time.Now(), 0)
	if transition.State != StateConfigError || transition.Code != "" {
		t.Fatalf("config_error transition = %#v", transition)
	}
	if machine.Current() != StateConfigError {
		t.Fatalf("machine state = %q, want %q", machine.Current(), StateConfigError)
	}
}

func TestMachine_StartingToPaused(t *testing.T) {
	machine := NewMachine()
	transition := machine.Transition("pause", time.Now(), 0)
	if transition.State != StatePaused || transition.Code != "paused" || !transition.ReleaseControls {
		t.Fatalf("pause transition = %#v", transition)
	}
	if machine.Current() != StatePaused {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_StartingToFetching(t *testing.T) {
	machine := NewMachine()
	transition := machine.Transition("start", time.Now(), 0)
	if transition.State != StateFetching {
		t.Fatalf("start transition = %#v", transition)
	}
	if machine.Current() != StateFetching {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_FetchingToActive(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	transition := machine.Transition("success", time.Now(), 0)
	if transition.State != StateActive || transition.Code != "" {
		t.Fatalf("success transition = %#v", transition)
	}
	if machine.Current() != StateActive {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_FetchingToDegraded_PreservesRunningTasks(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	transition := machine.Transition("failure", time.Now(), 10*time.Second)
	if transition.State != StateDegraded {
		t.Fatalf("failure transition = %#v", transition)
	}
	if machine.Current() != StateDegraded {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_DegradedToActive_Recovered(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	machine.Transition("failure", time.Now(), 10*time.Second)
	transition := machine.Transition("success", time.Now(), 0)
	if transition.State != StateActive || transition.Code != "recovered" {
		t.Fatalf("recovered transition = %#v", transition)
	}
	if machine.Current() != StateActive {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_DegradedToReleased_GraceExpired(t *testing.T) {
	machine := NewMachine()
	now := time.Now()
	machine.Transition("start", now, 0)
	machine.Transition("failure", now.Add(time.Second), 10*time.Second)
	transition := machine.Transition("grace_expired", now.Add(12*time.Second), 10*time.Second)
	if transition.State != StateReleased || transition.Code != "grace_expired" || !transition.ReleaseControls {
		t.Fatalf("grace_expired transition = %#v", transition)
	}
	if machine.Current() != StateReleased {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_ReleasedToActive(t *testing.T) {
	machine := NewMachine()
	now := time.Now()
	machine.Transition("start", now, 0)
	machine.Transition("failure", now.Add(time.Second), 10*time.Second)
	machine.Transition("grace_expired", now.Add(11*time.Second), 10*time.Second)
	transition := machine.Transition("success", now.Add(12*time.Second), 0)
	if transition.State != StateActive {
		t.Fatalf("released success transition = %#v", transition)
	}
	if machine.Current() != StateActive {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_Pause_RequiresPersistSuccess(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	_ = machine.Transition("failure", time.Now(), 10*time.Second)
	transition := machine.Transition("pause", time.Now(), 0)
	if transition.State != StatePaused || transition.Code != "paused" {
		t.Fatalf("pause transition = %#v", transition)
	}
	if machine.Current() != StatePaused {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_Resume_Manual(t *testing.T) {
	machine := NewMachine()
	machine.Transition("pause", time.Now(), 0)
	transition := machine.Transition("resume", time.Now(), 0)
	if transition.State != StateFetching || transition.Code != "resumed" || !transition.Reevaluate {
		t.Fatalf("resume transition = %#v", transition)
	}
	if machine.Current() != StateFetching {
		t.Fatalf("machine state = %q", machine.Current())
	}
}

func TestMachine_Resume_Expired(t *testing.T) {
	machine := NewMachine()
	machine.Transition("pause", time.Now(), 0)
	transition := machine.Transition("resume", time.Now(), 0)
	if transition.State != StateFetching || transition.Code != "resumed" {
		t.Fatalf("resume transition = %#v", transition)
	}
}

func TestMachine_ReloadConfigFailure_KeepsState(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	machine.Transition("failure", time.Now(), 10*time.Second)
	transition := machine.Transition("config_error", time.Now(), 0)
	if transition.State != StateDegraded || transition.Reevaluate || transition.ReleaseControls {
		t.Fatalf("config_error transition = %#v", transition)
	}
}

func TestMachine_PollTimeout_TreatedAsFailure(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	transition := machine.Transition("failure", time.Now(), 10*time.Second)
	if transition.State != StateDegraded {
		t.Fatalf("timeout treated as failure transition = %#v", transition)
	}
}

func TestMachine_WakeDetection_TriggersReevaluate(t *testing.T) {
	machine := NewMachine()
	transition := machine.Transition("wake", time.Now(), 0)
	if transition.State != StateStarting || !transition.Reevaluate {
		t.Fatalf("wake transition = %#v", transition)
	}
}

func TestMachine_MCPCrash_TreatedAsFailure(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	transition := machine.Transition("failure", time.Now(), 10*time.Second)
	if transition.State != StateDegraded {
		t.Fatalf("mcp crash transition = %#v", transition)
	}
}

func TestMachine_Pause_PersistFailure_NoTransition(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	_ = machine.Transition("pause", time.Now(), 0)
	transition := machine.Transition("start", time.Now(), 0)
	if transition.State != StatePaused {
		t.Fatalf("invalid transition after pause = %#v", transition)
	}
}

func TestMachine_ClockRewind_DoesNotFalsePositiveExpire(t *testing.T) {
	pause := Pause{Until: time.Now().Add(time.Minute)}
	if pause.Expired(time.Now().Add(-time.Minute)) {
		t.Fatal("pause should not expire when clock rewinds")
	}
}

func TestMachine_RollbackOnInvalidTransition(t *testing.T) {
	machine := NewMachine()
	machine.Transition("start", time.Now(), 0)
	transition := machine.Transition("config_error", time.Now(), 0)
	if transition.State != StateFetching {
		t.Fatalf("config_error from active: got %q, want %q", transition.State, StateFetching)
	}
}
