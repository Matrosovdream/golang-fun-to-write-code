package lb

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRoundRobinIsEven(t *testing.T) {
	bs := newBackends(3, Policy{})
	rr := &roundRobin{}
	counts := map[*Backend]int{}
	var order []*Backend
	for range 300 {
		b := rr.Pick("", bs)
		counts[b]++
		order = append(order, b)
	}
	for _, b := range bs {
		require.Equal(t, 100, counts[b])
	}
	require.Equal(t, order[:3], []*Backend{bs[0], bs[1], bs[2]})
	require.Equal(t, order[3:6], order[:3], "strict rotation")
}

// The sequence nginx documents for weights 5:1:1. A naive weighted round-robin
// would emit aaaaabc; the smooth one interleaves.
func TestSmoothWRRSequence(t *testing.T) {
	bs := newBackends(3, Policy{}, 5, 1, 1)
	w := newWRR(bs)
	names := map[*Backend]string{bs[0]: "a", bs[1]: "b", bs[2]: "c"}
	var seq strings.Builder
	for range 14 {
		seq.WriteString(names[w.Pick("", bs)])
	}
	require.Equal(t, "aabacaa"+"aabacaa", seq.String())
}

func TestWRRProportionsWithSubsetOfCandidates(t *testing.T) {
	bs := newBackends(3, Policy{}, 3, 2, 1)
	w := newWRR(bs)
	counts := map[*Backend]int{}
	for range 600 {
		counts[w.Pick("", bs)]++
	}
	require.Equal(t, []int{300, 200, 100}, []int{counts[bs[0]], counts[bs[1]], counts[bs[2]]})

	// Without the first backend (unhealthy) the rest split 2:1.
	counts = map[*Backend]int{}
	for range 300 {
		counts[w.Pick("", bs[1:])]++
	}
	require.InDelta(t, 200, counts[bs[1]], 2)
	require.InDelta(t, 100, counts[bs[2]], 2)
}

func TestWRREffectiveWeightDropsAndRecovers(t *testing.T) {
	bs := newBackends(2, Policy{}, 8, 8)
	w := newWRR(bs)
	w.Penalize(bs[0])
	require.Equal(t, 4, w.peers[bs[0]].effective, "a failure halves the effective weight")
	w.Penalize(bs[0])
	require.Equal(t, 2, w.peers[bs[0]].effective)

	counts := map[*Backend]int{}
	for range 6 {
		counts[w.Pick("", bs)]++
	}
	require.Greater(t, counts[bs[1]], counts[bs[0]], "the penalised backend gets less while recovering")

	for range 6 {
		w.Pick("", bs)
	}
	require.Equal(t, 8, w.peers[bs[0]].effective, "one point back per round until the configured weight")
}

func TestLeastConnPicksFewestInflight(t *testing.T) {
	bs := newBackends(3, Policy{})
	lc := &leastConn{}
	bs[0].Acquire()
	bs[0].Acquire()
	bs[1].Acquire()
	for range 5 {
		require.Same(t, bs[2], lc.Pick("", bs))
	}
	bs[2].Acquire()
	bs[2].Acquire()
	for range 5 {
		require.Same(t, bs[1], lc.Pick("", bs))
	}
	bs[1].Release()
	bs[0].Release()
	bs[0].Release()
	require.Same(t, bs[0], lc.Pick("", bs), "bs[0] is idle, the others are not")
}

func TestLeastConnSpreadsTies(t *testing.T) {
	bs := newBackends(3, Policy{})
	lc := &leastConn{}
	counts := map[*Backend]int{}
	for range 30 {
		counts[lc.Pick("", bs)]++
	}
	for _, b := range bs {
		require.Equal(t, 10, counts[b])
	}
}

func keys(n int) []string {
	ks := make([]string, n)
	for i := range ks {
		ks[i] = fmt.Sprintf("user-%d", i)
	}
	return ks
}

func assign(r *ring, cands []*Backend, ks []string) map[string]*Backend {
	m := make(map[string]*Backend, len(ks))
	for _, k := range ks {
		m[k] = r.Pick(k, cands)
	}
	return m
}

func TestRingIsStickyAndBalanced(t *testing.T) {
	bs := newBackends(5, Policy{})
	r := newRing(bs, 160)
	ks := keys(10000)
	first := assign(r, bs, ks)
	require.Equal(t, first, assign(r, bs, ks), "same key, same backend, every time")

	counts := map[*Backend]int{}
	for _, b := range first {
		counts[b]++
	}
	for _, b := range bs {
		t.Logf("%s owns %d of %d keys", b.URL.Host, counts[b], len(ks))
		// Random placement of 160 points per node: expect roughly +-1/sqrt(160), so allow 25%.
		require.InDelta(t, len(ks)/len(bs), counts[b], float64(len(ks)/len(bs))*0.25, b.URL.Host)
	}
}

func TestRingMinimalRemapWhenNodeLeaves(t *testing.T) {
	bs := newBackends(5, Policy{})
	ks := keys(10000)
	before := assign(newRing(bs, 160), bs, ks)

	gone, rest := bs[2], append(append([]*Backend{}, bs[:2]...), bs[3:]...)
	after := assign(newRing(rest, 160), rest, ks)

	moved := 0
	for _, k := range ks {
		if before[k] == after[k] {
			continue
		}
		moved++
		require.Same(t, gone, before[k], "key %s moved between surviving nodes", k)
	}
	owned := 0
	for _, b := range before {
		if b == gone {
			owned++
		}
	}
	require.Equal(t, owned, moved, "exactly the departed node's keys moved")
	t.Logf("removing 1 of 5 moved %d of %d keys (%.1f%%)", moved, len(ks), 100*float64(moved)/float64(len(ks)))
	require.InDelta(t, 0.2, float64(moved)/float64(len(ks)), 0.05)
}

// Health filtering through the candidate list must agree with physically
// rebuilding the ring without the node.
func TestRingCandidateFilterEqualsRebuild(t *testing.T) {
	bs := newBackends(5, Policy{})
	ks := keys(5000)
	full := newRing(bs, 160)
	rest := append(append([]*Backend{}, bs[:1]...), bs[2:]...)
	require.Equal(t, assign(newRing(rest, 160), rest, ks), assign(full, rest, ks))
}

func TestRingMinimalRemapWhenNodeJoins(t *testing.T) {
	bs := newBackends(5, Policy{})
	ks := keys(10000)
	before := assign(newRing(bs[:4], 160), bs[:4], ks)
	after := assign(newRing(bs, 160), bs, ks)

	moved := 0
	for _, k := range ks {
		if before[k] != after[k] {
			moved++
			require.Same(t, bs[4], after[k], "key %s moved to an old node", k)
		}
	}
	t.Logf("adding a 5th node moved %d of %d keys", moved, len(ks))
	require.InDelta(t, 0.2, float64(moved)/float64(len(ks)), 0.05)
}

func TestRingWeightScalesShare(t *testing.T) {
	bs := newBackends(2, Policy{}, 1, 3)
	r := newRing(bs, 160)
	counts := map[*Backend]int{}
	for _, k := range keys(10000) {
		counts[r.Pick(k, bs)]++
	}
	share := float64(counts[bs[1]]) / 10000
	t.Logf("weight 3 of 4 owns %.1f%% of keys", 100*share)
	require.InDelta(t, 0.75, share, 0.15, "weight 3 vs 1 should own about three quarters")
}
