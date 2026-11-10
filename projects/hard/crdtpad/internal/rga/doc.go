// Package rga implements a Replicated Growable Array: a sequence CRDT for text.
// It is pure state and logic; no I/O, no goroutines, no locking. A Doc is
// owned by exactly one goroutine at a time.
//
// The model, from the outside in:
//
//   - Every character ever typed is a node with a unique, totally ordered ID
//     (Lamport clock, replica name). Nodes form a linked list in document
//     order, starting from a sentinel head with the zero ID.
//   - Insert{after, id}: put the new node directly after `after`, then keep
//     moving right past every node whose ID is GREATER than the new one.
//     Viewed as a tree (parent = `after`), the list is a pre-order walk with
//     children ordered by descending ID. A Lamport clock guarantees that every
//     descendant has a greater ID than its ancestors, so "skip greater nodes"
//     steps over whole subtrees of greater siblings and stops at the first
//     smaller sibling (or the end of the parent's subtree). The resulting list
//     depends only on the SET of ops applied, never on their order, which is
//     what makes concurrent inserts at one spot converge to the same text.
//   - Delete{target}: set a tombstone. The node stays in the list and stays
//     addressable, so an insert "after" a character someone else deleted
//     concurrently still finds its parent. (Never reclaiming tombstones is
//     this project's scope guard; see the README.)
//   - Delivery: an op is applied only when it is the next one from its
//     replica (Seq == version[replica]+1) and the node it refers to exists.
//     Otherwise it waits in the pending buffer and is retried whenever
//     something else is applied. An op whose Seq is already covered by the
//     version vector is a duplicate and is ignored, so delivery may be
//     reordered, delayed and repeated.
//
// Position lookups walk the list, O(n); production editors use a balanced
// tree (or a skip list) keyed by visible position. The algorithm is the same.
package rga

import (
	"errors"
	"fmt"
	"maps"
	"strings"
)

var ErrPosition = errors.New("rga: position out of range")

// Version maps each replica to the highest Seq applied from it. Because
// Seqs are contiguous and applied in order, "I have everything up to n"
// is the whole story for that replica.
type Version map[string]uint64

type node struct {
	id   ID
	char rune
	dead bool
	next *node
}

type Doc struct {
	replica string
	head    *node
	nodes   map[ID]*node // every character ever inserted, tombstones included; zero ID is head
	clock   uint64       // highest Lamport clock seen
	version Version
	log     []Op // applied ops in application order, a valid causal order
	pending map[string]map[uint64]Op
}

// New returns an empty document edited by the given replica. The name must
// satisfy ValidReplica and must never be reused for a different history:
// two replicas sharing a name would hand out the same Seqs.
func New(replica string) *Doc {
	if !ValidReplica(replica) {
		panic(fmt.Sprintf("rga: invalid replica name %q", replica))
	}
	head := &node{}
	return &Doc{
		replica: replica,
		head:    head,
		nodes:   map[ID]*node{{}: head},
		version: Version{},
		pending: map[string]map[uint64]Op{},
	}
}

// Insert types char so that it becomes the character at visible index pos,
// and returns the op to broadcast.
func (d *Doc) Insert(pos int, char rune) (Op, error) {
	parent := d.visible(pos - 1)
	if parent == nil {
		return Op{}, fmt.Errorf("%w: insert at %d", ErrPosition, pos)
	}
	op := Op{
		Replica: d.replica,
		Seq:     d.version[d.replica] + 1,
		Kind:    KindInsert,
		Clock:   d.clock + 1,
		After:   parent.id,
		Char:    char,
	}
	d.integrate(op)
	return op, nil
}

// Delete removes the character at visible index pos and returns the op to broadcast.
func (d *Doc) Delete(pos int) (Op, error) {
	target := d.visible(pos)
	if pos < 0 || target == nil {
		return Op{}, fmt.Errorf("%w: delete at %d", ErrPosition, pos)
	}
	op := Op{Replica: d.replica, Seq: d.version[d.replica] + 1, Kind: KindDelete, Target: target.id}
	d.integrate(op)
	return op, nil
}

// Apply delivers an op from anywhere, in any order, any number of times. It
// returns the ops that were integrated as a result, in application order:
// empty for a duplicate or an op that must wait, several if it unblocked
// buffered ones. Callers persist and rebroadcast exactly that list.
func (d *Doc) Apply(op Op) ([]Op, error) {
	if err := op.validate(); err != nil {
		return nil, err
	}
	if op.Seq <= d.version[op.Replica] {
		return nil, nil
	}
	if d.pending[op.Replica] == nil {
		d.pending[op.Replica] = map[uint64]Op{}
	}
	d.pending[op.Replica][op.Seq] = op
	return d.drain(), nil
}

// drain applies buffered ops until none is ready. Only the op at
// version[r]+1 of each replica can be ready, so a pass is cheap.
func (d *Doc) drain() []Op {
	var applied []Op
	for progress := true; progress; {
		progress = false
		for r, q := range d.pending {
			op, ok := q[d.version[r]+1]
			if !ok || !d.ready(op) {
				continue
			}
			d.integrate(op)
			applied = append(applied, op)
			delete(q, op.Seq)
			if len(q) == 0 {
				delete(d.pending, r)
			}
			progress = true
		}
	}
	return applied
}

func (d *Doc) ready(op Op) bool {
	dep := op.After
	if op.Kind == KindDelete {
		dep = op.Target
	}
	return op.Seq == d.version[op.Replica]+1 && d.nodes[dep] != nil
}

func (d *Doc) integrate(op Op) {
	switch op.Kind {
	case KindInsert:
		n := &node{id: op.ID(), char: op.Char}
		prev := d.nodes[op.After]
		for prev.next != nil && prev.next.id.Compare(n.id) > 0 {
			prev = prev.next
		}
		n.next, prev.next = prev.next, n
		d.nodes[n.id] = n
		d.clock = max(d.clock, op.Clock)
	case KindDelete:
		d.nodes[op.Target].dead = true
	}
	d.version[op.Replica] = op.Seq
	d.log = append(d.log, op)
}

// Missing returns, in a valid causal order, the applied ops that a replica
// with version `since` has not seen.
func (d *Doc) Missing(since Version) []Op {
	var out []Op
	for _, op := range d.log {
		if op.Seq > since[op.Replica] {
			out = append(out, op)
		}
	}
	return out
}

// Version returns a copy of the current version vector.
func (d *Doc) Version() Version { return maps.Clone(d.version) }

// Pending is the number of received ops still waiting for their predecessors.
func (d *Doc) Pending() int {
	n := 0
	for _, q := range d.pending {
		n += len(q)
	}
	return n
}

func (d *Doc) Text() string {
	var sb strings.Builder
	for n := d.head.next; n != nil; n = n.next {
		if !n.dead {
			sb.WriteRune(n.char)
		}
	}
	return sb.String()
}

// Len is the number of visible characters.
func (d *Doc) Len() int {
	count := 0
	for n := d.head.next; n != nil; n = n.next {
		if !n.dead {
			count++
		}
	}
	return count
}

// visible returns the node of the i-th visible character; i == -1 is the head.
func (d *Doc) visible(i int) *node {
	if i < -1 {
		return nil
	}
	n := d.head
	for ; i >= 0 && n != nil; i-- {
		n = n.next
		for n != nil && n.dead {
			n = n.next
		}
	}
	return n
}
