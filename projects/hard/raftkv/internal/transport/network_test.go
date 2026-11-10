package transport_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"raftkv/internal/clock"
	"raftkv/internal/raft"
	"raftkv/internal/transport"
)

type inbox struct {
	mu   sync.Mutex
	msgs []raft.Message
}

func (i *inbox) Receive(m raft.Message) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.msgs = append(i.msgs, m)
}

func (i *inbox) count() int {
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.msgs)
}

// net3 is a network of nodes 1..3 whose inboxes the test can inspect.
func net3(seed uint64) (*transport.Network, *clock.Fake, map[raft.NodeID]*inbox) {
	clk := clock.NewFake()
	n := transport.NewNetwork(clk, seed)
	boxes := map[raft.NodeID]*inbox{}
	for id := raft.NodeID(1); id <= 3; id++ {
		boxes[id] = &inbox{}
		n.Attach(id, boxes[id])
	}
	return n, clk, boxes
}

func msg(from, to raft.NodeID) raft.Message { return raft.Message{From: from, To: to, Term: 1} }

func TestNetworkDelivers(t *testing.T) {
	n, _, boxes := net3(1)
	n.Endpoint(1).Send(msg(1, 2))
	require.Equal(t, 1, boxes[2].count())
	require.Equal(t, 0, boxes[3].count())
}

func TestNetworkDropDupDelay(t *testing.T) {
	n, clk, boxes := net3(1)

	n.SetFaults(transport.Faults{Drop: 1})
	n.Endpoint(1).Send(msg(1, 2))
	require.Equal(t, 0, boxes[2].count())

	n.SetFaults(transport.Faults{Dup: 1})
	n.Endpoint(1).Send(msg(1, 2))
	require.Equal(t, 2, boxes[2].count())

	n.SetFaults(transport.Faults{Delay: 100 * time.Millisecond})
	n.Endpoint(1).Send(msg(1, 3))
	clk.Advance(99 * time.Millisecond)
	require.Equal(t, 0, boxes[3].count(), "still in flight")
	clk.Advance(time.Millisecond)
	require.Equal(t, 1, boxes[3].count())
}

func TestNetworkLinkFaultsAreDirected(t *testing.T) {
	n, _, boxes := net3(1)
	n.SetLinkFaults(1, 2, transport.Faults{Drop: 1})
	n.Endpoint(1).Send(msg(1, 2))
	n.Endpoint(2).Send(msg(2, 1))
	n.Endpoint(1).Send(msg(1, 3))
	require.Equal(t, 0, boxes[2].count())
	require.Equal(t, 1, boxes[1].count())
	require.Equal(t, 1, boxes[3].count())
}

func TestNetworkPartitionIsolateHeal(t *testing.T) {
	n, clk, boxes := net3(1)
	n.SetFaults(transport.Faults{Delay: 50 * time.Millisecond})

	n.Endpoint(1).Send(msg(1, 2)) // in flight when the cut happens
	n.Isolate(1)
	n.Endpoint(1).Send(msg(1, 3))
	n.Endpoint(2).Send(msg(2, 3)) // 2 and 3 still talk
	clk.Advance(time.Second)
	require.Equal(t, 0, boxes[2].count(), "the cut also kills messages already in flight")
	require.Equal(t, 1, boxes[3].count())

	n.Partition([]raft.NodeID{1, 2}) // 3 is in no group: alone with the other ungrouped nodes
	n.Endpoint(1).Send(msg(1, 2))
	n.Endpoint(1).Send(msg(1, 3))
	clk.Advance(time.Second)
	require.Equal(t, 1, boxes[2].count())
	require.Equal(t, 1, boxes[3].count())

	n.Heal()
	n.Endpoint(1).Send(msg(1, 3))
	clk.Advance(time.Second)
	require.Equal(t, 2, boxes[3].count())
}

func TestNetworkDetachedNodeReceivesNothing(t *testing.T) {
	n, _, boxes := net3(1)
	n.Detach(2)
	n.Endpoint(1).Send(msg(1, 2))
	require.Equal(t, 0, boxes[2].count())
}
