package job

import (
	"encoding/json"
	"fmt"
	"time"
)

type State string

const (
	StatePending State = "pending"
	StateRunning State = "running"
	StateDone    State = "done"
	StateDead    State = "dead" // exhausted all attempts → dead-letter queue
)

// transitions is the state machine: an explicit map instead of scattered
// if-checks, so illegal moves (done → running) fail loudly.
var transitions = map[State][]State{
	StatePending: {StateRunning},
	StateRunning: {StateDone, StatePending, StateDead}, // Pending = retry
	StateDone:    {},
	StateDead:    {},
}

type Priority string

const (
	PriorityHigh    Priority = "high"
	PriorityDefault Priority = "default"
	PriorityLow     Priority = "low"
)

type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    Priority        `json:"priority"`
	State       State           `json:"state"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	RunAt       time.Time       `json:"run_at"`
	LastError   string          `json:"last_error,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
}

func (j *Job) TransitionTo(next State) error {
	for _, allowed := range transitions[j.State] {
		if next == allowed {
			j.State = next
			return nil
		}
	}
	return fmt.Errorf("illegal transition %s → %s for job %s", j.State, next, j.ID)
}

// Backoff returns the delay before the next attempt: 1s, 2s, 4s, 8s...
// capped at a minute. Attempts start at 1.
func (j *Job) Backoff() time.Duration {
	d := time.Second << (j.Attempts - 1)
	if d > time.Minute {
		return time.Minute
	}
	return d
}
