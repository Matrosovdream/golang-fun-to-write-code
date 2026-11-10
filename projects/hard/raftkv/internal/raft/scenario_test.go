package raft_test

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/kv"
	"raftkv/internal/raft"
)

func TestElection(t *testing.T) {
	for _, n := range []int{3, 5} {
		t.Run(fmt.Sprintf("%d nodes", n), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				c := newCluster(t, n, uint64(n), nil)
				defer c.stop()

				leader := c.waitLeader()
				term := c.status(leader).Term
				c.run(10 * time.Second) // no faults: leadership must be stable

				require.Equal(t, leader, c.waitLeader())
				for _, id := range c.ids {
					st := c.status(id)
					require.Equal(t, term, st.Term, "node %d: a healthy cluster must not hold new elections", id)
					require.Equal(t, leader, st.Leader, "node %d", id)
				}
			})
		})
	}
}

func TestLeaderCrash(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCluster(t, 3, 10, nil)
		defer c.stop()

		old := c.waitLeader()
		oldTerm := c.status(old).Term
		c.mustExec(old, put("x", "1"))
		c.run(time.Second) // followers learn the commit index from heartbeats
		c.crash(old)

		survivors := without(c.ids, old)
		next := c.waitLeader(survivors...)
		require.Greater(t, c.status(next).Term, oldTerm)
		require.Equal(t, kv.Result{Value: "1", Found: true}, c.mustExec(next, get("x")), "committed data must survive")
		c.mustExec(next, put("y", "2"))

		c.restart(old)
		c.run(5 * time.Second)
		st := c.status(old)
		require.Equal(t, raft.Follower, st.Role)
		require.Equal(t, next, st.Leader)
		c.requireAgree(c.ids, "x", "y")
		require.Equal(t, kv.Result{Value: "2", Found: true}, c.members[old].mach.read("y"), "the restarted node catches up")
	})
}

// splitBrain isolates the leader plus one follower (a minority of 5) and has
// the old leader accept a write it can never commit.
type splitBrain struct {
	*cluster
	old      raft.NodeID
	minority []raft.NodeID
	majority []raft.NodeID
	stale    <-chan outcome // the write the old leader accepted but cannot commit
	newLead  raft.NodeID
}

func newSplitBrain(t *testing.T, seed uint64) *splitBrain {
	c := newCluster(t, 5, seed, nil)
	s := &splitBrain{cluster: c, old: c.waitLeader()}
	c.mustExec(s.old, put("base", "0"))
	c.run(time.Second)

	buddy := without(c.ids, s.old)[0]
	s.minority = []raft.NodeID{s.old, buddy}
	s.majority = without(c.ids, s.minority...)
	c.net.Partition(s.minority, s.majority)

	s.stale, _ = c.async(s.old, put("lost", "minority"))
	c.run(3 * time.Second)
	s.newLead = c.waitLeader(s.majority...)
	return s
}

func TestMinorityPartitionCannotCommit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSplitBrain(t, 20)
		defer s.stop()

		before := s.status(s.old).CommitIndex
		s.run(5 * time.Second)
		select {
		case o := <-s.stale:
			t.Fatalf("minority leader answered a write it cannot commit: %+v", o)
		default:
		}
		require.Equal(t, raft.Leader, s.status(s.old).Role, "without CheckQuorum the cut-off leader still believes it leads")
		require.Equal(t, before, s.status(s.old).CommitIndex)
		_, err := s.exec(s.old, put("also-lost", "x"))
		require.Error(t, err)

		require.Equal(t, kv.Result{}, s.mustExec(s.newLead, put("won", "majority")), "the majority commits")
		require.Equal(t, kv.Result{Value: "majority", Found: true}, s.mustExec(s.newLead, get("won")))
		require.False(t, s.members[s.old].mach.read("won").Found, "the minority never saw it")
	})
}

func TestHealOverwritesStaleLeaderEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := newSplitBrain(t, 21)
		defer s.stop()
		hasLost := func(id raft.NodeID) bool {
			for _, e := range s.log(id) {
				if strings.Contains(string(e.Data), "lost") {
					return true
				}
			}
			return false
		}
		require.True(t, hasLost(s.old), "precondition: the stale leader holds an uncommitted entry")
		s.mustExec(s.newLead, put("won", "majority"))

		s.net.Heal()
		o, ok := s.await(s.stale, 10*time.Second)
		require.True(t, ok)
		require.ErrorIs(t, o.err, raft.ErrNotLeader, "a deposed leader must never ack a write that was overwritten")

		s.run(5 * time.Second)
		for _, id := range s.ids {
			require.False(t, hasLost(id), "node %d still has the overwritten entry", id)
		}
		s.requireAgree(s.ids, "base", "won", "lost")
		for _, id := range s.ids[1:] {
			require.Equal(t, s.log(s.ids[0]), s.log(id), "after healing every log is identical")
		}
		require.Equal(t, raft.Follower, s.status(s.old).Role)
	})
}

func TestRestartKeepsCommittedEntries(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dir := t.TempDir()
		c := newCluster(t, 3, 30, func(id raft.NodeID) raft.Storage {
			s, err := raft.OpenFileStorage(filepath.Join(dir, fmt.Sprintf("%d.wal", id)))
			require.NoError(t, err)
			return s
		})
		defer c.stop()

		leader := c.waitLeader()
		term := c.status(leader).Term
		for _, k := range []string{"a", "b", "c"} {
			c.mustExec(leader, put(k, k+"!"))
		}
		c.run(time.Second)

		for _, id := range c.ids { // a power cut for the whole cluster
			c.crash(id)
		}
		for _, id := range c.ids {
			c.start(id)
		}
		next := c.waitLeader()
		require.Greater(t, c.status(next).Term, term, "terms never go backwards across a restart")
		for _, k := range []string{"a", "b", "c"} {
			require.Equal(t, kv.Result{Value: k + "!", Found: true}, c.mustExec(next, get(k)))
		}
	})
}

func TestRetriedCommandIsNotAppliedTwice(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		c := newCluster(t, 3, 40, nil)
		defer c.stop()
		old := c.waitLeader()

		// A's put commits, but the client never sees the reply and will retry it.
		a := kv.Command{Op: kv.Put, Key: "x", Value: "from-A", ClientID: "A", Seq: 1}
		c.mustExec(old, a)
		c.mustExec(old, kv.Command{Op: kv.Put, Key: "x", Value: "from-B", ClientID: "B", Seq: 1})
		c.run(time.Second)
		c.crash(old)

		next := c.waitLeader(without(c.ids, old)...)
		res := c.mustExec(next, a) // the retry reaches the new leader
		require.Equal(t, kv.Result{}, res, "the retry gets the original answer: x did not exist then")
		require.Equal(t, kv.Result{Value: "from-B", Found: true}, c.mustExec(next, get("x")), "the retry must not overwrite B's later write")
	})
}
