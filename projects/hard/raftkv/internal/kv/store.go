// Package kv is the replicated state machine: a string map plus a table that
// remembers each client's last command so retries are not applied twice.
package kv

import (
	"context"
	"encoding/json"
	"fmt"
)

type Op string

const (
	Put    Op = "put"
	Get    Op = "get"
	Delete Op = "delete"
)

// Command is what goes into the Raft log. (ClientID, Seq) identifies a
// client's request across retries; ClientID "" opts out of deduplication.
type Command struct {
	Op       Op     `json:"op"`
	Key      string `json:"key"`
	Value    string `json:"value,omitempty"`
	ClientID string `json:"client,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
}

// Result describes the key as it was before the command ran, so for a get it
// is the answer, and for a put or delete the value that was overwritten.
type Result struct {
	Value string
	Found bool
}

type session struct {
	seq    uint64
	result Result
}

// Store implements raft.StateMachine. Only the Raft loop goroutine calls it,
// so it needs no locking.
type Store struct {
	data     map[string]string
	sessions map[string]session // clientID -> last executed command and its answer
}

func NewStore() *Store {
	return &Store{data: make(map[string]string), sessions: make(map[string]session)}
}

// Apply executes one committed command. A command whose Seq the client has
// already had executed (a retry after a timeout or a leader change) is
// answered from the session table instead of running again. This assumes a
// client has one request in flight and numbers them upward.
func (s *Store) Apply(_ uint64, data []byte) any {
	var c Command
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("kv: bad command: %w", err) // ignored by every replica alike, so they stay in sync
	}
	if sess, ok := s.sessions[c.ClientID]; ok && c.Seq <= sess.seq {
		return sess.result
	}
	res := s.exec(c)
	if c.ClientID != "" {
		s.sessions[c.ClientID] = session{seq: c.Seq, result: res}
	}
	return res
}

func (s *Store) exec(c Command) Result {
	v, found := s.data[c.Key]
	switch c.Op {
	case Put:
		s.data[c.Key] = c.Value
	case Delete:
		delete(s.data, c.Key)
	}
	return Result{Value: v, Found: found}
}

// Proposer is the slice of *raft.Node that clients need.
type Proposer interface {
	Propose(ctx context.Context, cmd []byte) (any, error)
}

// Exec sends c through consensus and returns the state machine's answer.
// Reads go through the log too, which makes them linearizable: a read is
// ordered with every write, and a deposed leader can never answer one.
func Exec(ctx context.Context, p Proposer, c Command) (Result, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return Result{}, fmt.Errorf("kv: encode command: %w", err)
	}
	v, err := p.Propose(ctx, b)
	if err != nil {
		return Result{}, err
	}
	switch r := v.(type) {
	case Result:
		return r, nil
	case error:
		return Result{}, r
	default:
		return Result{}, fmt.Errorf("kv: unexpected apply result %T", v)
	}
}
