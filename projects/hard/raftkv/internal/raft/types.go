package raft

import (
	"errors"

	"raftkv/internal/clock"
)

var (
	ErrNotLeader = errors.New("raft: not the leader")
	ErrStopped   = errors.New("raft: node stopped")
)

type NodeID uint64

type Role uint8

const (
	Follower Role = iota
	Candidate
	Leader
)

func (r Role) String() string { return [...]string{"follower", "candidate", "leader"}[r] }

func (r Role) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// Entry is one log slot. The index is its position (the first entry has index 1).
type Entry struct {
	Term uint64 `json:"term"`
	Data []byte `json:"data,omitempty"` // empty: the no-op a new leader appends
}

type MsgType uint8

const (
	MsgRequestVote MsgType = iota + 1
	MsgRequestVoteResp
	MsgAppendEntries
	MsgAppendEntriesResp
)

// Message is one flat wire struct for all four message kinds (the Raft paper's
// two RPCs, each split into request and response). Requests and responses are
// independent one-way messages, so the network may drop, delay, duplicate or
// reorder either of them and the node never blocks on a peer.
type Message struct {
	Type MsgType `json:"type"`
	From NodeID  `json:"from"`
	To   NodeID  `json:"to"`
	Term uint64  `json:"term"`

	// RequestVote
	LastLogIndex uint64 `json:"lastLogIndex,omitempty"`
	LastLogTerm  uint64 `json:"lastLogTerm,omitempty"`

	// AppendEntries
	PrevLogIndex uint64  `json:"prevLogIndex,omitempty"`
	PrevLogTerm  uint64  `json:"prevLogTerm,omitempty"`
	Entries      []Entry `json:"entries,omitempty"`
	LeaderCommit uint64  `json:"leaderCommit,omitempty"`

	// Responses. Success: vote granted / entries appended. Index (append
	// responses only): on success the highest index now known to match the
	// leader, on failure the follower's last log index, a back-off hint.
	Success bool   `json:"success,omitempty"`
	Index   uint64 `json:"index,omitempty"`
}

// Transport sends best-effort and never blocks; delivery is not guaranteed.
type Transport interface {
	Send(Message)
}

// StateMachine consumes committed commands, in log order, exactly once per
// node incarnation. It is called on the node's loop goroutine and must be
// deterministic. It must start empty: after a restart Raft replays the
// committed log into it.
type StateMachine interface {
	Apply(index uint64, cmd []byte) any
}

type Config struct {
	ID        NodeID
	Peers     []NodeID // the whole cluster, this node included
	Storage   Storage
	Machine   StateMachine
	Transport Transport
	Clock     clock.Clock
	Seed      uint64                    // seeds the election-timeout jitter
	OnRole    func(term uint64, r Role) // optional; runs on the loop goroutine
}

// Status is a snapshot of the loop-owned state.
type Status struct {
	ID          NodeID `json:"id"`
	Term        uint64 `json:"term"`
	Role        Role   `json:"role"`
	Leader      NodeID `json:"leader"` // 0 = unknown
	LastIndex   uint64 `json:"lastIndex"`
	CommitIndex uint64 `json:"commitIndex"`
	LastApplied uint64 `json:"lastApplied"`
}
