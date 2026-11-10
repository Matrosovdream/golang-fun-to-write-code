// Package transport carries raft.Messages: Network is an in-memory network
// with fault injection for tests, HTTP is the real thing.
package transport

import (
	"math/rand/v2"
	"sync"
	"time"

	"raftkv/internal/clock"
	"raftkv/internal/raft"
)

// Receiver is where a delivered message goes (a *raft.Node).
type Receiver interface {
	Receive(raft.Message)
}

// Faults describe one directed link. Delay and Jitter are measured on the
// network's clock, so with a fake clock "slow" costs no wall time.
type Faults struct {
	Drop   float64 // probability a message is lost
	Dup    float64 // probability it is delivered twice
	Delay  time.Duration
	Jitter time.Duration // extra random delay in [0, Jitter]
}

type link struct {
	Faults
	rng *rand.Rand // one stream per directed link, so its fault schedule depends only on the seed
}

// Network is a simulated network between nodes. Tests attach a Receiver per
// node and give each node its Endpoint as the raft.Transport.
type Network struct {
	clock clock.Clock
	seed  uint64

	mu    sync.Mutex
	nodes map[raft.NodeID]Receiver
	links map[[2]raft.NodeID]*link
	base  Faults              // for links not yet created
	group map[raft.NodeID]int // partition group; nodes in different groups cannot talk
}

func NewNetwork(clk clock.Clock, seed uint64) *Network {
	return &Network{
		clock: clk, seed: seed,
		nodes: make(map[raft.NodeID]Receiver),
		links: make(map[[2]raft.NodeID]*link),
		group: make(map[raft.NodeID]int),
	}
}

type endpoint struct {
	n    *Network
	from raft.NodeID
}

func (e endpoint) Send(m raft.Message) { e.n.send(m) }

// Endpoint returns the raft.Transport for node id.
func (n *Network) Endpoint(id raft.NodeID) raft.Transport { return endpoint{n, id} }

// Attach connects (or, after a restart, replaces) the node behind id.
func (n *Network) Attach(id raft.NodeID, r Receiver) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.nodes[id] = r
}

// Detach models a crashed machine: messages to it vanish.
func (n *Network) Detach(id raft.NodeID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	delete(n.nodes, id)
}

// SetFaults applies f to every link, existing and future.
func (n *Network) SetFaults(f Faults) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.base = f
	for _, l := range n.links {
		l.Faults = f
	}
}

// SetLinkFaults applies f to the directed link from -> to only.
func (n *Network) SetLinkFaults(from, to raft.NodeID, f Faults) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.link(from, to).Faults = f
}

// Partition splits the network: nodes listed in the same group can talk to
// each other, nodes in different groups cannot, and nodes in no group form one
// more group together. Heal undoes it.
func (n *Network) Partition(groups ...[]raft.NodeID) {
	n.mu.Lock()
	defer n.mu.Unlock()
	clear(n.group)
	for i, g := range groups {
		for _, id := range g {
			n.group[id] = i + 1
		}
	}
}

// Isolate cuts one node off from everyone else.
func (n *Network) Isolate(id raft.NodeID) { n.Partition([]raft.NodeID{id}) }

func (n *Network) Heal() { n.Partition() }

func (n *Network) link(from, to raft.NodeID) *link {
	k := [2]raft.NodeID{from, to}
	l, ok := n.links[k]
	if !ok {
		l = &link{Faults: n.base, rng: rand.New(rand.NewPCG(n.seed, uint64(from)<<32|uint64(to)))}
		n.links[k] = l
	}
	return l
}

func (n *Network) send(m raft.Message) {
	n.mu.Lock()
	if n.group[m.From] != n.group[m.To] {
		n.mu.Unlock()
		return
	}
	l := n.link(m.From, m.To)
	if l.rng.Float64() < l.Drop {
		n.mu.Unlock()
		return
	}
	delays := []time.Duration{l.delay()}
	if l.rng.Float64() < l.Dup {
		delays = append(delays, l.delay())
	}
	n.mu.Unlock()

	for _, d := range delays {
		if d <= 0 {
			n.deliver(m)
		} else {
			n.clock.AfterFunc(d, func() { n.deliver(m) })
		}
	}
}

func (l *link) delay() time.Duration {
	return l.Delay + time.Duration(l.rng.Int64N(int64(l.Jitter)+1))
}

// deliver re-checks the partition, so cutting a link also kills the messages
// already in flight on it.
func (n *Network) deliver(m raft.Message) {
	n.mu.Lock()
	dst, ok := n.nodes[m.To]
	if n.group[m.From] != n.group[m.To] {
		ok = false
	}
	n.mu.Unlock()
	if ok {
		dst.Receive(m)
	}
}
