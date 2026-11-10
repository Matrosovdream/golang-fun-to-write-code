package raft_test

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"raftkv/internal/kv"
	"raftkv/internal/raft"
)

// op is one completed client operation on a single key. call and ret are
// stamps from one global counter taken just before the request was first sent
// and just after the answer came back, so call(B) > ret(A) really means B
// started after A had finished.
type op struct {
	kind      kv.Op
	arg       string    // the value a put wrote
	out       kv.Result // what the cluster answered: the key's state before the op
	call, ret int64
}

func (o op) String() string {
	return fmt.Sprintf("[%d,%d] %s(%q) -> %+v", o.call, o.ret, o.kind, o.arg, o.out)
}

type history struct {
	stamp atomic.Int64
	mu    sync.Mutex
	byKey map[string][]op
}

func (h *history) begin() int64 { return h.stamp.Add(1) }

func (h *history) end(key string, o op) {
	o.ret = h.stamp.Add(1)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byKey == nil {
		h.byKey = make(map[string][]op)
	}
	h.byKey[key] = append(h.byKey[key], o)
}

// linearizable reports whether the operations on one key can be put in a
// single order that (a) respects real time: an op that finished before another
// began comes first, and (b) is a legal run of a register. It is a depth-first
// search over "which unfinished op goes next", memoised on the set of ops
// already placed plus the register's value (Wing & Gong, with Lowe's memo).
func linearizable(ops []op) bool {
	if len(ops) > 62 {
		panic("history too long for the bitmask search")
	}
	type state struct {
		placed  uint64
		val     string
		present bool
	}
	failed := map[state]bool{}
	var search func(s state) bool
	search = func(s state) bool {
		if s.placed == 1<<len(ops)-1 {
			return true
		}
		if failed[s] {
			return false
		}
		for i, o := range ops {
			if s.placed>>i&1 == 1 || !canGoNext(ops, s.placed, i) {
				continue
			}
			if o.out != (kv.Result{Value: s.val, Found: s.present}) {
				continue
			}
			next := state{placed: s.placed | 1<<i, val: s.val, present: s.present}
			switch o.kind {
			case kv.Put:
				next.val, next.present = o.arg, true
			case kv.Delete:
				next.val, next.present = "", false
			}
			if search(next) {
				return true
			}
		}
		failed[s] = true
		return false
	}
	return search(state{})
}

// canGoNext: op i may be placed next only if no unplaced op finished before i began.
func canGoNext(ops []op, placed uint64, i int) bool {
	for j, p := range ops {
		if j != i && placed>>j&1 == 0 && p.ret < ops[i].call {
			return false
		}
	}
	return true
}

func TestLinearizableChecker(t *testing.T) {
	r := func(v string, found bool) kv.Result { return kv.Result{Value: v, Found: found} }
	tests := []struct {
		name string
		ops  []op
		ok   bool
	}{
		{"sequential", []op{
			{kv.Put, "1", r("", false), 1, 2},
			{kv.Get, "", r("1", true), 3, 4},
			{kv.Delete, "", r("1", true), 5, 6},
			{kv.Get, "", r("", false), 7, 8},
		}, true},
		{"lost acknowledged write", []op{
			{kv.Put, "1", r("", false), 1, 2},
			{kv.Get, "", r("", false), 3, 4},
		}, false},
		{"stale read after a newer write", []op{
			{kv.Put, "1", r("", false), 1, 2},
			{kv.Put, "2", r("1", true), 3, 4},
			{kv.Get, "", r("1", true), 5, 6},
		}, false},
		{"overlapping read may see either side", []op{
			{kv.Put, "1", r("", false), 1, 6},
			{kv.Get, "", r("", false), 2, 3},
			{kv.Get, "", r("1", true), 4, 5},
		}, true},
		{"reads must not flip back", []op{
			{kv.Put, "1", r("", false), 1, 10},
			{kv.Get, "", r("1", true), 2, 3},
			{kv.Get, "", r("", false), 4, 5},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.ok, linearizable(tt.ops))
		})
	}
}

func TestLeaderTrackerCatchesTwoLeaders(t *testing.T) {
	var lt leaderTracker
	require.NoError(t, lt.observe(1, 3, raft.Leader))
	require.NoError(t, lt.observe(2, 3, raft.Follower))
	require.NoError(t, lt.observe(2, 4, raft.Leader))
	err := lt.observe(3, 3, raft.Leader)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "term 3"))
}
