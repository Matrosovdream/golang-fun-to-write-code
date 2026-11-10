package raft_test

import (
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/clock"
	"raftkv/internal/kv"
	"raftkv/internal/raft"
	"raftkv/internal/transport"
)

// Every cluster test runs inside a synctest bubble on a clock.Fake: time moves
// only when cluster.step advances it, and synctest.Wait then returns once every
// goroutine (nodes, clients) has finished reacting and is blocked. That gives
// "a minute of cluster time in milliseconds" and no sleeps anywhere.
const step = 10 * time.Millisecond

// machine is the kv state machine as seen by tests: it adds a lock so tests can
// read it, and reports each applied entry to the cluster's safety check.
type machine struct {
	mu      sync.Mutex
	store   *kv.Store
	onApply func(index uint64, data []byte)
}

func (m *machine) Apply(index uint64, data []byte) any {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onApply(index, data)
	return m.store.Apply(index, data)
}

func (m *machine) read(key string) kv.Result {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.store.Apply(0, []byte(fmt.Sprintf(`{"op":"get","key":%q}`, key))).(kv.Result)
}

// leaderTracker is the safety invariant "at most one leader per term". Nodes
// report every role change into it, so it samples every state change of every
// test, not just the moments a test happens to look.
type leaderTracker struct {
	mu      sync.Mutex
	leaders map[uint64]raft.NodeID
}

func (lt *leaderTracker) observe(id raft.NodeID, term uint64, r raft.Role) error {
	if r != raft.Leader {
		return nil
	}
	lt.mu.Lock()
	defer lt.mu.Unlock()
	if lt.leaders == nil {
		lt.leaders = make(map[uint64]raft.NodeID)
	}
	if prev, ok := lt.leaders[term]; ok {
		return fmt.Errorf("election safety violated: node %d and node %d both became leader of term %d", prev, id, term)
	}
	lt.leaders[term] = id
	return nil
}

func (lt *leaderTracker) maxTerm() (m uint64) {
	lt.mu.Lock()
	defer lt.mu.Unlock()
	for term := range lt.leaders {
		m = max(m, term)
	}
	return m
}

type member struct {
	store  raft.Storage
	node   *raft.Node // nil while crashed
	mach   *machine
	cancel context.CancelFunc
	done   chan struct{}
}

type cluster struct {
	t      *testing.T
	seed   uint64
	clock  *clock.Fake
	net    *transport.Network
	ids    []raft.NodeID
	ctx    context.Context // cancelled by stop; wakes clients so a failed test ends cleanly
	cancel context.CancelFunc

	leaders leaderTracker

	mu      sync.Mutex
	members map[raft.NodeID]*member
	applied map[uint64]string // state machine safety: the entry each log index applied, on any node
}

// newCluster starts n nodes (ids 1..n). storage may be nil for in-memory disks.
func newCluster(t *testing.T, n int, seed uint64, storage func(raft.NodeID) raft.Storage) *cluster {
	c := &cluster{t: t, seed: seed, clock: clock.NewFake(), members: map[raft.NodeID]*member{}, applied: map[uint64]string{}}
	c.net = transport.NewNetwork(c.clock, seed)
	c.ctx, c.cancel = context.WithCancel(context.Background())
	for i := range n {
		c.ids = append(c.ids, raft.NodeID(i+1))
	}
	for _, id := range c.ids {
		var s raft.Storage = &raft.MemStorage{}
		if storage != nil {
			s = storage(id)
		}
		c.members[id] = &member{store: s}
		c.start(id)
	}
	return c
}

func (c *cluster) checkApplied(index uint64, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prev, ok := c.applied[index]; ok && prev != string(data) {
		c.t.Errorf("state machine safety violated: index %d applied %q and %q", index, prev, data)
	}
	c.applied[index] = string(data)
}

// start builds a fresh Node from the member's Storage: that is a restart.
func (c *cluster) start(id raft.NodeID) {
	m := c.members[id]
	mach := &machine{store: kv.NewStore(), onApply: c.checkApplied}
	node, err := raft.New(raft.Config{
		ID: id, Peers: c.ids, Storage: m.store, Machine: mach,
		Transport: c.net.Endpoint(id), Clock: c.clock, Seed: c.seed,
		OnRole: func(term uint64, r raft.Role) {
			if err := c.leaders.observe(id, term, r); err != nil {
				c.t.Error(err)
			}
		},
	})
	require.NoError(c.t, err)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		node.Run(ctx)
	}()
	c.net.Attach(id, node)
	c.mu.Lock()
	m.node, m.mach, m.cancel, m.done = node, mach, cancel, done
	c.mu.Unlock()
	synctest.Wait() // Run has registered its ticker
}

// crash throws the Node away and keeps its Storage.
func (c *cluster) crash(id raft.NodeID) {
	c.mu.Lock()
	m := c.members[id]
	node := m.node
	m.node = nil
	c.mu.Unlock()
	if node == nil {
		return
	}
	c.net.Detach(id)
	m.cancel()
	<-m.done
}

func (c *cluster) restart(id raft.NodeID) {
	c.crash(id)
	c.start(id)
}

func (c *cluster) node(id raft.NodeID) *raft.Node {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.members[id].node
}

func (c *cluster) crashed() (out []raft.NodeID) {
	for _, id := range c.ids {
		if c.node(id) == nil {
			out = append(out, id)
		}
	}
	return out
}

func (c *cluster) stop() {
	c.cancel()
	for _, id := range c.ids {
		c.crash(id)
		if cl, ok := c.members[id].store.(io.Closer); ok {
			cl.Close()
		}
	}
}

func (c *cluster) step() {
	c.clock.Advance(step)
	synctest.Wait()
}

func (c *cluster) run(d time.Duration) {
	for range int(d / step) {
		c.step()
	}
}

func (c *cluster) until(max time.Duration, what string, cond func() bool) {
	c.t.Helper()
	for range int(max / step) {
		if cond() {
			return
		}
		c.step()
	}
	c.t.Fatalf("gave up after %v of cluster time waiting for %s", max, what)
}

// sleep blocks the calling client goroutine on the fake clock.
func (c *cluster) sleep(d time.Duration) {
	ch := make(chan struct{})
	c.clock.AfterFunc(d, func() { close(ch) })
	select {
	case <-ch:
	case <-c.ctx.Done():
	}
}

func (c *cluster) status(id raft.NodeID) raft.Status {
	if node := c.node(id); node != nil {
		return node.Status()
	}
	return raft.Status{}
}

// leaderAmong returns the single live leader among ids (all nodes if none given).
func (c *cluster) leaderAmong(ids ...raft.NodeID) (raft.NodeID, bool) {
	if len(ids) == 0 {
		ids = c.ids
	}
	var leaders []raft.NodeID
	for _, id := range ids {
		if c.status(id).Role == raft.Leader {
			leaders = append(leaders, id)
		}
	}
	if len(leaders) != 1 {
		return 0, false
	}
	return leaders[0], true
}

func (c *cluster) waitLeader(ids ...raft.NodeID) raft.NodeID {
	c.t.Helper()
	var leader raft.NodeID
	c.until(30*time.Second, "a single leader", func() bool {
		var ok bool
		leader, ok = c.leaderAmong(ids...)
		return ok
	})
	return leader
}

type outcome struct {
	res kv.Result
	err error
}

// async proposes cmd on node id from a new goroutine. The returned cancel
// abandons the proposal; the caller must drive time (run/await) to see it finish.
func (c *cluster) async(id raft.NodeID, cmd kv.Command) (<-chan outcome, context.CancelFunc) {
	ch := make(chan outcome, 1)
	node := c.node(id)
	ctx, cancel := context.WithCancel(c.ctx)
	go func() {
		res, err := kv.Exec(ctx, node, cmd)
		ch <- outcome{res, err}
	}()
	return ch, cancel
}

func (c *cluster) await(ch <-chan outcome, max time.Duration) (outcome, bool) {
	for range int(max / step) {
		select {
		case o := <-ch:
			return o, true
		default:
		}
		c.step()
	}
	return outcome{}, false
}

// exec runs cmd on node id and drives time until it answers or 10s pass.
func (c *cluster) exec(id raft.NodeID, cmd kv.Command) (kv.Result, error) {
	ch, cancel := c.async(id, cmd)
	defer cancel()
	if o, ok := c.await(ch, 10*time.Second); ok {
		return o.res, o.err
	}
	cancel()
	<-ch
	return kv.Result{}, context.DeadlineExceeded
}

func (c *cluster) mustExec(id raft.NodeID, cmd kv.Command) kv.Result {
	c.t.Helper()
	res, err := c.exec(id, cmd)
	require.NoError(c.t, err)
	return res
}

// drive runs fn on its own goroutine and advances time until it returns.
func (c *cluster) drive(max time.Duration, fn func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	c.until(max, "driven work to finish", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
}

func (c *cluster) log(id raft.NodeID) []raft.Entry {
	_, log, err := c.members[id].store.Load()
	require.NoError(c.t, err)
	return log
}

// requireAgree checks that ids hold the same committed prefix of the log and
// the same value for every key.
func (c *cluster) requireAgree(ids []raft.NodeID, keys ...string) {
	c.t.Helper()
	ref := ids[0]
	rs, rlog := c.status(ref), c.log(ref)
	for _, id := range ids[1:] {
		st := c.status(id)
		require.Equal(c.t, rs.CommitIndex, st.CommitIndex, "commit index of node %d vs %d", id, ref)
		require.Equal(c.t, rlog[:rs.CommitIndex], c.log(id)[:st.CommitIndex], "committed prefix of node %d vs %d", id, ref)
		for _, k := range keys {
			require.Equal(c.t, c.members[ref].mach.read(k), c.members[id].mach.read(k), "key %q on node %d vs %d", k, id, ref)
		}
	}
}

func without(ids []raft.NodeID, drop ...raft.NodeID) []raft.NodeID {
	return slices.DeleteFunc(slices.Clone(ids), func(id raft.NodeID) bool { return slices.Contains(drop, id) })
}

func put(k, v string) kv.Command { return kv.Command{Op: kv.Put, Key: k, Value: v} }
func get(k string) kv.Command    { return kv.Command{Op: kv.Get, Key: k} }
