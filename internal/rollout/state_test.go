package rollout

import "testing"

func TestAllowedTransitions(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{StatePending, StateValidating},
		{StateValidating, StateReady},
		{StateReady, StateDeploying},
		{StateDeploying, StateEvaluating},
		{StateDeploying, StatePaused},
		{StateDeploying, StateRollingBack},
		{StateEvaluating, StatePromoting},
		{StateEvaluating, StatePaused},
		{StateEvaluating, StateRollingBack},
		{StateEvaluating, StateCompleted},
		{StatePromoting, StateDeploying},
		{StatePaused, StateEvaluating},
		{StatePaused, StateRollingBack},
		{StateRollingBack, StateRolledBack},
	}

	for _, tt := range tests {
		if err := Transition(tt.from, tt.to); err != nil {
			t.Fatalf("expected transition %s -> %s to be allowed: %v", tt.from, tt.to, err)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	tests := []struct {
		from State
		to   State
	}{
		{StateCompleted, StateDeploying},
		{StateRolledBack, StateDeploying},
		{StateFailed, StateReady},
		{StatePending, StateDeploying},
		{StatePending, StateFailed},
		{StateValidating, StateRollingBack},
		{StateReady, StateRollingBack},
		{StateReady, StateCompleted},
	}

	for _, tt := range tests {
		if err := Transition(tt.from, tt.to); err == nil {
			t.Fatalf("expected transition %s -> %s to be rejected", tt.from, tt.to)
		}
	}
}

func TestTerminalStates(t *testing.T) {
	for _, state := range []State{StateCompleted, StateRolledBack, StateFailed} {
		if !state.Terminal() {
			t.Fatalf("expected %s to be terminal", state)
		}
	}

	if StateEvaluating.Terminal() {
		t.Fatal("expected EVALUATING to be non-terminal")
	}
}
