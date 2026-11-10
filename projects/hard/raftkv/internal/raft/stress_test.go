package raft_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/kv"
	"raftkv/internal/raft"
	"raftkv/internal/transport"
)

const (
	attemptTimeout = time.Second
	retryDelay     = 20 * time.Millisecond
)

// clerk is a client that never gives up: it resends the same (clientID, seq)
// to one node after another until some leader answers.
type clerk struct {
	c        *cluster
	id       string
	seq      uint64
	rng      *rand.Rand
	hist     *history
	attempts int
}

func (k *clerk) do(kind kv.Op, key, value string) kv.Result {
	k.seq++
	cmd := kv.Command{Op: kind, Key: key, Value: value, ClientID: k.id, Seq: k.seq}
	call := k.hist.begin()
	n := len(k.c.ids)
	for target := k.rng.IntN(n); k.c.ctx.Err() == nil; target = (target + 1) % n {
		k.attempts++
		if node := k.c.node(k.c.ids[target]); node != nil {
			ctx, cancel := context.WithCancel(k.c.ctx)
			k.c.clock.AfterFunc(attemptTimeout, cancel)
			res, err := kv.Exec(ctx, node, cmd)
			cancel()
			if err == nil {
				k.hist.end(key, op{kind: kind, arg: value, out: res, call: call})
				return res
			}
		}
		k.c.sleep(retryDelay)
	}
	return kv.Result{}
}

func (k *clerk) randomOp(keys []string) {
	key := keys[k.rng.IntN(len(keys))]
	switch p := k.rng.IntN(10); {
	case p < 5:
		k.do(kv.Put, key, fmt.Sprintf("%s-%d", k.id, k.seq+1))
	case p < 9:
		k.do(kv.Get, key, "")
	default:
		k.do(kv.Delete, key, "")
	}
}

// nemesis does one random nasty thing to the cluster.
func (c *cluster) nemesis(rng *rand.Rand) {
	pick := func(ids []raft.NodeID) raft.NodeID { return ids[rng.IntN(len(ids))] }
	switch p := rng.IntN(100); {
	case p < 30: // split into a random minority of 1-2 nodes and the rest
		ids := slices.Clone(c.ids)
		rng.Shuffle(len(ids), func(i, j int) { ids[i], ids[j] = ids[j], ids[i] })
		k := 1 + rng.IntN(len(ids)/2)
		c.net.Partition(ids[:k], ids[k:])
	case p < 45:
		c.net.Isolate(pick(c.ids))
	case p < 65:
		c.net.Heal()
	case p < 80: // crash, but never more than a minority at once
		if down := c.crashed(); len(down) < len(c.ids)/2 {
			c.crash(pick(without(c.ids, down...)))
		}
	default:
		if down := c.crashed(); len(down) > 0 {
			c.start(pick(down))
		}
	}
}

// TestStress runs concurrent clients against five nodes while the network
// drops, duplicates and delays messages and a nemesis partitions and crashes
// nodes. Afterwards: every client finished, every node agrees, and each key's
// history is linearizable, which includes "every acknowledged write is visible
// unless a later acknowledged write replaced it". The election-safety and
// state-machine-safety checks run throughout, as in every cluster test.
//
// Replay a failure with RAFTKV_SEED=<seed>. The seed fixes the fault schedule;
// goroutine scheduling can still differ between runs.
func TestStress(t *testing.T) {
	seeds := []uint64{1, 2, 3}
	if s := os.Getenv("RAFTKV_SEED"); s != "" {
		seed, err := strconv.ParseUint(s, 10, 64)
		require.NoError(t, err)
		seeds = []uint64{seed}
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Logf("seed=%d (replay: RAFTKV_SEED=%d go test -run TestStress ./internal/raft)", seed, seed)
			synctest.Test(t, func(t *testing.T) { stress(t, seed) })
		})
	}
}

func stress(t *testing.T, seed uint64) {
	const (
		clients      = 4
		opsPerClient = 30
		chaos        = 20 * time.Second
	)
	keys := []string{"a", "b", "c"}
	c := newCluster(t, 5, seed, nil)
	defer c.stop()
	rng := rand.New(rand.NewPCG(seed, 1000))
	c.net.SetFaults(transport.Faults{Drop: 0.10, Dup: 0.05, Delay: 10 * time.Millisecond, Jitter: 30 * time.Millisecond})

	hist := &history{}
	newClerk := func(id string, stream uint64) *clerk {
		return &clerk{c: c, id: id, rng: rand.New(rand.NewPCG(seed, stream)), hist: hist}
	}
	clerks := make([]*clerk, clients)
	var wg sync.WaitGroup
	for i := range clerks {
		clerks[i] = newClerk(fmt.Sprintf("client-%d", i), uint64(i))
		wg.Go(func() {
			for range opsPerClient {
				clerks[i].randomOp(keys)
			}
		})
	}
	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	for elapsed := time.Duration(0); elapsed < chaos; {
		d := time.Duration(200+rng.IntN(1300)) * time.Millisecond
		c.run(d)
		elapsed += d
		c.nemesis(rng)
	}

	// Calm: heal everything, bring every node back, and the clients must finish.
	c.net.Heal()
	c.net.SetFaults(transport.Faults{})
	for _, id := range c.crashed() {
		c.start(id)
	}
	c.until(2*time.Minute, "all clients to finish", func() bool {
		select {
		case <-finished:
			return true
		default:
			return false
		}
	})
	c.waitLeader()

	// Final reads, issued after everything else completed, pin the end state.
	final := newClerk("final", 99)
	c.drive(time.Minute, func() {
		for _, k := range keys {
			final.do(kv.Get, k, "")
		}
	})
	c.run(5 * time.Second)

	total, attempts := 0, final.attempts
	for _, cl := range clerks {
		attempts += cl.attempts
	}
	for key, ops := range hist.byKey {
		total += len(ops)
		require.True(t, linearizable(ops), "history of key %q is not linearizable:\n%v", key, ops)
	}
	t.Logf("%d acknowledged ops, %d attempts, highest term %d", total, attempts, c.leaders.maxTerm())
	require.Equal(t, clients*opsPerClient+len(keys), total)
	c.requireAgree(c.ids, keys...)
}
