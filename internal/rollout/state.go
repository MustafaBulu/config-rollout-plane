package rollout

import "fmt"

type State string

const (
	StatePending     State = "PENDING"
	StateValidating  State = "VALIDATING"
	StateReady       State = "READY"
	StateDeploying   State = "DEPLOYING"
	StateEvaluating  State = "EVALUATING"
	StatePromoting   State = "PROMOTING"
	StatePaused      State = "PAUSED"
	StateCompleted   State = "COMPLETED"
	StateRollingBack State = "ROLLING_BACK"
	StateRolledBack  State = "ROLLED_BACK"
	StateFailed      State = "FAILED"
)

var allowedTransitions = map[State]map[State]struct{}{
	StatePending: {
		StateValidating: {},
	},
	StateValidating: {
		StateReady: {},
	},
	StateReady: {
		StateDeploying: {},
	},
	StateDeploying: {
		StateEvaluating:  {},
		StateRollingBack: {},
	},
	StateEvaluating: {
		StatePromoting:   {},
		StatePaused:      {},
		StateRollingBack: {},
		StateCompleted:   {},
	},
	StatePromoting: {
		StateDeploying:   {},
		StateRollingBack: {},
	},
	StatePaused: {
		StateEvaluating:  {},
		StateRollingBack: {},
	},
	StateRollingBack: {
		StateRolledBack: {},
	},
}

func CanTransition(from State, to State) bool {
	next, ok := allowedTransitions[from]
	if !ok {
		return false
	}
	_, ok = next[to]
	return ok
}

func Transition(from State, to State) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("invalid rollout state transition: %s -> %s", from, to)
}

func (s State) Terminal() bool {
	return s == StateCompleted || s == StateRolledBack || s == StateFailed
}
