package raft

import (
	"slices"
	"sync"
)

// HardState is what Raft must persist before replying to anyone: forgetting a
// vote lets a node vote twice in one term, which can elect two leaders.
type HardState struct {
	Term     uint64 `json:"term"`
	VotedFor NodeID `json:"votedFor"`
}

// Storage is the node's disk. Each method must be durable when it returns.
// A crash in tests is "drop the Node, keep the Storage".
type Storage interface {
	Load() (HardState, []Entry, error)
	SaveHardState(HardState) error
	Append([]Entry) error
	TruncateFrom(index uint64) error // drops entries with index >= the argument
}

// MemStorage survives a simulated crash because the test keeps the pointer.
// Its mutex exists for tests that read it while a node is running.
type MemStorage struct {
	mu  sync.Mutex
	hs  HardState
	log []Entry
}

func (s *MemStorage) Load() (HardState, []Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hs, slices.Clone(s.log), nil
}

func (s *MemStorage) SaveHardState(hs HardState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hs = hs
	return nil
}

func (s *MemStorage) Append(es []Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = append(s.log, es...)
	return nil
}

func (s *MemStorage) TruncateFrom(index uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = s.log[:min(index-1, uint64(len(s.log)))]
	return nil
}
