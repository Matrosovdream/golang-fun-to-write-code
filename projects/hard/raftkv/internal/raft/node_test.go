package raft_test

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"

	"raftkv/internal/clock"
	"raftkv/internal/raft"
)

// solo is one node under test with every neighbour played by the test: it
// feeds the node hand-made messages and reads exactly what the node sends back.
type solo struct {
	t     *testing.T
	node  *raft.Node
	clock *clock.Fake
	store *raft.MemStorage

	mu      sync.Mutex
	sent    []raft.Message
	applied []string
	cancel  context.CancelFunc
	done    chan struct{}
}

func (s *solo) Send(m raft.Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, m)
}

func (s *solo) Apply(_ uint64, cmd []byte) any {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, string(cmd))
	return nil
}

// newSolo starts node 1 of {1,2,3} on store, which may already hold state.
func newSolo(t *testing.T, store *raft.MemStorage) *solo {
	s := &solo{t: t, clock: clock.NewFake(), store: store, done: make(chan struct{})}
	node, err := raft.New(raft.Config{
		ID: 1, Peers: []raft.NodeID{1, 2, 3}, Storage: store, Machine: s,
		Transport: s, Clock: s.clock, Seed: 1,
	})
	require.NoError(t, err)
	s.node = node
	var ctx context.Context
	ctx, s.cancel = context.WithCancel(context.Background())
	go func() {
		defer close(s.done)
		node.Run(ctx)
	}()
	synctest.Wait()
	return s
}

func (s *solo) stop() {
	s.cancel()
	<-s.done
}

// recv delivers m and returns everything the node sent in response.
func (s *solo) recv(m raft.Message) []raft.Message {
	m.To = 1
	s.node.Receive(m)
	synctest.Wait()
	return s.take()
}

func (s *solo) take() []raft.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.sent
	s.sent = nil
	return out
}

// electionTimeout ticks until the node campaigns and returns its new term.
func (s *solo) electionTimeout() uint64 {
	for range 40 {
		s.clock.Advance(raft.TickInterval)
		synctest.Wait()
		for _, m := range s.take() {
			if m.Type == raft.MsgRequestVote {
				return m.Term
			}
		}
	}
	s.t.Fatal("node never campaigned")
	return 0
}

// elect makes the node leader of the term it campaigns in, with node 2's vote.
func (s *solo) elect() uint64 {
	term := s.electionTimeout()
	s.recv(raft.Message{Type: raft.MsgRequestVoteResp, From: 2, Term: term, Success: true})
	require.Equal(s.t, raft.Leader, s.node.Status().Role)
	return term
}

func seeded(hs raft.HardState, terms ...uint64) *raft.MemStorage {
	s := &raft.MemStorage{}
	s.SaveHardState(hs)
	var es []raft.Entry
	for _, term := range terms {
		es = append(es, raft.Entry{Term: term, Data: []byte("cmd")})
	}
	s.Append(es)
	return s
}

func terms(es []raft.Entry) (out []uint64) {
	for _, e := range es {
		out = append(out, e.Term)
	}
	return out
}

func TestRequestVoteRules(t *testing.T) {
	// The voter is in term 2 with log terms [1 2].
	tests := []struct {
		name      string
		votedFor  raft.NodeID
		term      uint64
		lastIndex uint64
		lastTerm  uint64
		grant     bool
		wantTerm  uint64
	}{
		{"stale term", 0, 1, 2, 2, false, 2},
		{"already voted for someone else", 3, 2, 2, 2, false, 2},
		{"already voted for this candidate", 2, 2, 2, 2, true, 2},
		{"candidate's last term is older", 0, 2, 9, 1, false, 2},
		{"same last term but shorter log", 0, 2, 1, 2, false, 2},
		{"equal log", 0, 2, 2, 2, true, 2},
		{"newer last term beats a longer log", 0, 2, 1, 3, true, 2},
		{"higher term clears the old vote", 3, 3, 2, 2, true, 3},
		{"higher term but stale log: adopt the term, refuse the vote", 3, 3, 1, 1, false, 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSolo(t, seeded(raft.HardState{Term: 2, VotedFor: tt.votedFor}, 1, 2))
				defer s.stop()
				out := s.recv(raft.Message{Type: raft.MsgRequestVote, From: 2, Term: tt.term, LastLogIndex: tt.lastIndex, LastLogTerm: tt.lastTerm})
				require.Len(t, out, 1)
				require.Equal(t, raft.MsgRequestVoteResp, out[0].Type)
				require.Equal(t, tt.grant, out[0].Success)
				require.Equal(t, tt.wantTerm, out[0].Term)

				hs, _, _ := s.store.Load()
				require.Equal(t, tt.wantTerm, hs.Term, "the term was persisted before the reply")
				if tt.grant {
					require.Equal(t, raft.NodeID(2), hs.VotedFor, "the vote was persisted before the reply")
				}
			})
		})
	}
}

func TestVoteSurvivesRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		disk := &raft.MemStorage{}
		vote := func(s *solo, from raft.NodeID) bool {
			out := s.recv(raft.Message{Type: raft.MsgRequestVote, From: from, Term: 5})
			require.Len(t, out, 1)
			return out[0].Success
		}

		s := newSolo(t, disk)
		require.True(t, vote(s, 2))
		s.stop() // crash: the node is gone, only its disk is left

		s = newSolo(t, disk)
		defer s.stop()
		require.Equal(t, uint64(5), s.node.Status().Term)
		require.False(t, vote(s, 3), "a restarted node must not vote twice in one term")
		require.True(t, vote(s, 2), "but it still honours the vote it gave")
	})
}

func TestAnyHigherTermDemotes(t *testing.T) {
	tests := []struct {
		name string
		prep func(*solo)
		msg  raft.Message
	}{
		{"leader sees an append response", func(s *solo) { s.elect() }, raft.Message{Type: raft.MsgAppendEntriesResp, From: 3}},
		{"candidate sees a vote response", func(s *solo) { s.electionTimeout() }, raft.Message{Type: raft.MsgRequestVoteResp, From: 3}},
		{"leader sees an append request", func(s *solo) { s.elect() }, raft.Message{Type: raft.MsgAppendEntries, From: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSolo(t, &raft.MemStorage{})
				defer s.stop()
				tt.prep(s)
				tt.msg.Term = 9
				s.recv(tt.msg)

				st := s.node.Status()
				require.Equal(t, raft.Follower, st.Role)
				require.Equal(t, uint64(9), st.Term)
				hs, _, _ := s.store.Load()
				require.Equal(t, raft.HardState{Term: 9}, hs)
			})
		})
	}
}

func TestAppendEntriesLogMatching(t *testing.T) {
	// The follower's log terms are [1 1 2].
	tests := []struct {
		name       string
		term       uint64
		prevIndex  uint64
		prevTerm   uint64
		entries    []raft.Entry
		success    bool
		index      uint64
		wantLogLen int
		wantLeader raft.NodeID
	}{
		{"matches and appends", 2, 3, 2, []raft.Entry{{Term: 2}}, true, 4, 4, 2},
		{"heartbeat at the tail", 2, 3, 2, nil, true, 3, 3, 2},
		{"prev index past our end: hint is our last index", 2, 5, 2, []raft.Entry{{Term: 2}}, false, 3, 3, 2},
		{"prev term differs", 2, 3, 1, []raft.Entry{{Term: 2}}, false, 3, 3, 2},
		{"stale leader is refused", 1, 3, 2, []raft.Entry{{Term: 1}}, false, 0, 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				s := newSolo(t, seeded(raft.HardState{Term: 2}, 1, 1, 2))
				defer s.stop()
				out := s.recv(raft.Message{Type: raft.MsgAppendEntries, From: 2, Term: tt.term,
					PrevLogIndex: tt.prevIndex, PrevLogTerm: tt.prevTerm, Entries: tt.entries})
				require.Len(t, out, 1)
				require.Equal(t, tt.success, out[0].Success)
				require.Equal(t, tt.index, out[0].Index)
				require.Equal(t, uint64(2), out[0].Term)
				_, log, _ := s.store.Load()
				require.Len(t, log, tt.wantLogLen)
				require.Equal(t, tt.wantLeader, s.node.Status().Leader)
			})
		})
	}
}

func TestAppendEntriesTruncatesOnlyOnConflict(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSolo(t, seeded(raft.HardState{Term: 1}, 1, 1, 1, 1))
		defer s.stop()
		ae := func(prev uint64, es ...raft.Entry) raft.Message {
			return raft.Message{Type: raft.MsgAppendEntries, From: 2, Term: 1, PrevLogIndex: prev, PrevLogTerm: 1, Entries: es}
		}
		logTerms := func() []uint64 { _, log, _ := s.store.Load(); return terms(log) }

		// A delayed duplicate carrying a prefix of what we already hold must not shorten the log.
		out := s.recv(ae(1, raft.Entry{Term: 1}, raft.Entry{Term: 1}))
		require.True(t, out[0].Success)
		require.Equal(t, uint64(3), out[0].Index, "we ack only what the message proved, not our whole log")
		require.Equal(t, []uint64{1, 1, 1, 1}, logTerms())

		// A real conflict at index 3 replaces it and everything after it.
		s.recv(raft.Message{Type: raft.MsgAppendEntries, From: 2, Term: 2, PrevLogIndex: 2, PrevLogTerm: 1, Entries: []raft.Entry{{Term: 2}}})
		require.Equal(t, []uint64{1, 1, 2}, logTerms())
	})
}

func TestCommitIndexNeverMovesBackwards(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSolo(t, seeded(raft.HardState{Term: 1}, 1, 1, 1, 1))
		defer s.stop()
		ae := func(commit uint64, es ...raft.Entry) {
			s.recv(raft.Message{Type: raft.MsgAppendEntries, From: 2, Term: 1, LeaderCommit: commit, Entries: es})
		}
		ae(4, raft.Entry{Term: 1}, raft.Entry{Term: 1}, raft.Entry{Term: 1}, raft.Entry{Term: 1})
		require.Equal(t, uint64(4), s.node.Status().CommitIndex)

		ae(1, raft.Entry{Term: 1}) // an old duplicate, delivered late
		require.Equal(t, uint64(4), s.node.Status().CommitIndex)
	})
}

func TestFigure8CommitsOnlyCurrentTermEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSolo(t, seeded(raft.HardState{Term: 2}, 2)) // an entry from term 2 that never committed
		defer s.stop()
		term := s.elect()
		require.Equal(t, uint64(3), term)
		ack := func(index uint64) {
			s.recv(raft.Message{Type: raft.MsgAppendEntriesResp, From: 2, Term: term, Success: true, Index: index})
		}

		ack(1) // a majority (nodes 1 and 2) now stores the old entry...
		require.Equal(t, uint64(0), s.node.Status().CommitIndex, "...but a term-2 entry must not be committed by counting replicas")
		require.Empty(t, s.applied)

		ack(2) // replicating the leader's own no-op commits it, and the old entry with it
		require.Equal(t, uint64(2), s.node.Status().CommitIndex)
		require.Equal(t, []string{"cmd"}, s.applied)
	})
}

func TestDeposedLeaderFailsPendingProposals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSolo(t, &raft.MemStorage{})
		defer s.stop()
		s.elect()

		result := make(chan error, 1)
		go func() {
			_, err := s.node.Propose(context.Background(), []byte("write"))
			result <- err
		}()
		synctest.Wait()
		require.Empty(t, result, "no majority has the entry yet, so it must not be answered")

		s.recv(raft.Message{Type: raft.MsgAppendEntries, From: 3, Term: 9})
		require.ErrorIs(t, <-result, raft.ErrNotLeader)

		_, err := s.node.Propose(context.Background(), []byte("again"))
		require.ErrorIs(t, err, raft.ErrNotLeader)
	})
}
