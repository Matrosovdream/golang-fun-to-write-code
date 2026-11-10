package lb

import (
	"fmt"
	"sync"
	"sync/atomic"
)

const (
	RoundRobin         = "round_robin"
	WeightedRoundRobin = "weighted_round_robin"
	LeastConn          = "least_conn"
	ConsistentHash     = "consistent_hash"
)

var Strategies = []string{RoundRobin, WeightedRoundRobin, LeastConn, ConsistentHash}

// Balancer picks one of candidates, which the caller has already filtered to
// available backends (never empty). key is only used by consistent hashing.
type Balancer interface {
	Pick(key string, candidates []*Backend) *Backend
}

func NewBalancer(strategy string, backends []*Backend) (Balancer, error) {
	switch strategy {
	case RoundRobin:
		return &roundRobin{}, nil
	case WeightedRoundRobin:
		return newWRR(backends), nil
	case LeastConn:
		return &leastConn{}, nil
	case ConsistentHash:
		return newRing(backends, 160), nil
	}
	return nil, fmt.Errorf("unknown strategy %q", strategy)
}

type roundRobin struct{ next atomic.Uint64 }

func (r *roundRobin) Pick(_ string, c []*Backend) *Backend {
	return c[(r.next.Add(1)-1)%uint64(len(c))]
}

type leastConn struct{ next atomic.Uint64 }

// Pick scans from a rotating start so that ties (the common case when idle)
// spread across backends instead of always landing on the first one.
func (l *leastConn) Pick(_ string, c []*Backend) *Backend {
	start := int(l.next.Add(1) % uint64(len(c)))
	best := c[start]
	for i := 1; i < len(c); i++ {
		if b := c[(start+i)%len(c)]; b.Inflight() < best.Inflight() {
			best = b
		}
	}
	return best
}

type peer struct{ weight, effective, current int }

// wrr is nginx's smooth weighted round-robin. Every round each candidate's
// current weight grows by its effective weight; the largest wins and pays back
// the sum of all effective weights. With weights 5:1:1 that yields a a b a c a a
// rather than a a a a a b c, so a heavy backend never gets a long burst.
//
// The effective weight is the nginx twist: a failure halves it, and each round
// it climbs back by one until it reaches the configured weight again.
type wrr struct {
	mu    sync.Mutex
	peers map[*Backend]*peer
}

func newWRR(backends []*Backend) *wrr {
	w := &wrr{peers: make(map[*Backend]*peer, len(backends))}
	for _, b := range backends {
		w.peers[b] = &peer{weight: b.Weight, effective: b.Weight}
	}
	return w
}

func (w *wrr) Pick(_ string, c []*Backend) *Backend {
	w.mu.Lock()
	defer w.mu.Unlock()
	var best *Backend
	var bestPeer *peer
	total := 0
	for _, b := range c {
		p := w.peers[b]
		p.current += p.effective
		total += p.effective
		if p.effective < p.weight {
			p.effective++
		}
		if bestPeer == nil || p.current > bestPeer.current {
			best, bestPeer = b, p
		}
	}
	bestPeer.current -= total
	return best
}

func (w *wrr) Penalize(b *Backend) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.peers[b].effective /= 2
}
