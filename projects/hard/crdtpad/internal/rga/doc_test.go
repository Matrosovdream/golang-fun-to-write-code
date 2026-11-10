package rga

import (
	"encoding/json"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustInsert(t *testing.T, d *Doc, pos int, ch rune) Op {
	t.Helper()
	op, err := d.Insert(pos, ch)
	require.NoError(t, err)
	return op
}

func mustDelete(t *testing.T, d *Doc, pos int) Op {
	t.Helper()
	op, err := d.Delete(pos)
	require.NoError(t, err)
	return op
}

func typeString(t *testing.T, d *Doc, pos int, s string) []Op {
	t.Helper()
	var ops []Op
	for i, r := range []rune(s) {
		ops = append(ops, mustInsert(t, d, pos+i, r))
	}
	return ops
}

func applyAll(t *testing.T, d *Doc, ops ...Op) {
	t.Helper()
	for _, op := range ops {
		_, err := d.Apply(op)
		require.NoError(t, err)
	}
}

func permutations[T any](s []T) [][]T {
	if len(s) <= 1 {
		return [][]T{slices.Clone(s)}
	}
	var out [][]T
	for i := range s {
		rest := slices.Concat(s[:i:i], s[i+1:])
		for _, p := range permutations(rest) {
			out = append(out, append([]T{s[i]}, p...))
		}
	}
	return out
}

func reversed[T any](s []T) []T {
	r := slices.Clone(s)
	slices.Reverse(r)
	return r
}

func TestLocalEdits(t *testing.T) {
	d := New("a")
	typeString(t, d, 0, "held")
	mustInsert(t, d, 3, 'l')
	mustInsert(t, d, 5, 'o')
	require.Equal(t, "helldo", d.Text())
	mustDelete(t, d, 4)
	mustDelete(t, d, 0)
	require.Equal(t, "ello", d.Text())
	require.Equal(t, 4, d.Len())

	for _, pos := range []int{-1, 5, 99} {
		_, err := d.Insert(pos, 'x')
		require.ErrorIs(t, err, ErrPosition, "insert at %d", pos)
	}
	for _, pos := range []int{-1, 4, 99} {
		_, err := d.Delete(pos)
		require.ErrorIs(t, err, ErrPosition, "delete at %d", pos)
	}
}

func TestSingleReplicaMatchesSliceModel(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 7))
	d := New("a")
	var model []rune
	for range 3000 {
		if len(model) > 0 && rng.IntN(3) == 0 {
			pos := rng.IntN(len(model))
			mustDelete(t, d, pos)
			model = slices.Delete(model, pos, pos+1)
		} else {
			pos, ch := rng.IntN(len(model)+1), rune('a'+rng.IntN(26))
			mustInsert(t, d, pos, ch)
			model = slices.Insert(model, pos, ch)
		}
	}
	require.Equal(t, string(model), d.Text())
}

// Three replicas share "ab" and each types one character between a and b
// without hearing from the others. Equal clocks, so replica name breaks the
// tie and the greater ID lands leftmost: Z (C) before Y (B) before X (A).
func TestConcurrentInsertsAtOneSpot(t *testing.T) {
	a, b, c := New("A"), New("B"), New("C")
	base := typeString(t, a, 0, "ab")
	applyAll(t, b, base...)
	applyAll(t, c, base...)

	x := mustInsert(t, a, 1, 'X')
	y := mustInsert(t, b, 1, 'Y')
	z := mustInsert(t, c, 1, 'Z')
	require.Equal(t, x.Clock, y.Clock)
	require.Equal(t, y.Clock, z.Clock)

	applyAll(t, a, z, y)
	applyAll(t, b, x, z)
	applyAll(t, c, y, x)
	for _, d := range []*Doc{a, b, c} {
		require.Equal(t, "aZYXb", d.Text())
	}

	for _, order := range permutations([]Op{x, y, z}) {
		fresh := New("D")
		applyAll(t, fresh, base...)
		applyAll(t, fresh, order...)
		require.Equal(t, "aZYXb", fresh.Text())
	}
}

// Two people typing a word each at the same spot must not interleave letters.
func TestConcurrentRunsDoNotInterleave(t *testing.T) {
	a, b := New("A"), New("B")
	abc := typeString(t, a, 0, "abc")
	xyz := typeString(t, b, 0, "xyz")
	applyAll(t, a, xyz...)
	applyAll(t, b, abc...)
	require.Equal(t, "xyzabc", a.Text())
	require.Equal(t, "xyzabc", b.Text())
}

// A deletes b while B types X after b. The tombstone keeps b addressable, so
// X survives in either delivery order.
func TestConcurrentDeleteAndInsertAfterDeleted(t *testing.T) {
	a, b := New("A"), New("B")
	base := typeString(t, a, 0, "abc")
	applyAll(t, b, base...)

	del := mustDelete(t, a, 1)
	ins := mustInsert(t, b, 2, 'X')
	require.Equal(t, "ac", a.Text())
	require.Equal(t, "abXc", b.Text())

	applyAll(t, a, ins)
	applyAll(t, b, del)
	require.Equal(t, "aXc", a.Text())
	require.Equal(t, "aXc", b.Text())
}

// Ops from two replicas with cross-replica dependencies; every one of the 120
// delivery orders must end in the same text with an empty pending buffer.
func TestEveryDeliveryOrderConverges(t *testing.T) {
	a, b := New("A"), New("B")
	ops := typeString(t, a, 0, "ab")            // A1 a, A2 b
	applyAll(t, b, ops...)                      //
	ops = append(ops, mustInsert(t, b, 2, 'X')) // B1: after b
	ops = append(ops, mustDelete(t, a, 0))      // A3: deletes a
	ops = append(ops, mustInsert(t, b, 1, 'Y')) // B2: after a, concurrent with A3

	for _, order := range permutations(ops) {
		d := New("D")
		applyAll(t, d, order...)
		require.Zero(t, d.Pending(), "order %v", order)
		require.Equal(t, "YbX", d.Text(), "order %v", order)
	}
}

func TestPendingBufferReleasesInOrder(t *testing.T) {
	a := New("A")
	ops := typeString(t, a, 0, "ab")
	del := mustDelete(t, a, 0)

	b := New("B")
	applied, err := b.Apply(del)
	require.NoError(t, err)
	require.Empty(t, applied)
	applied, err = b.Apply(ops[1])
	require.NoError(t, err)
	require.Empty(t, applied)
	require.Equal(t, 2, b.Pending())
	require.Empty(t, b.Text())

	applied, err = b.Apply(ops[0])
	require.NoError(t, err)
	require.Equal(t, []Op{ops[0], ops[1], del}, applied)
	require.Zero(t, b.Pending())
	require.Equal(t, "b", b.Text())
}

func TestApplyIsIdempotent(t *testing.T) {
	a := New("A")
	ops := typeString(t, a, 0, "hey")
	ops = append(ops, mustDelete(t, a, 1))

	b := New("B")
	for _, op := range slices.Concat(ops, ops, reversed(ops)) {
		_, err := b.Apply(op)
		require.NoError(t, err)
	}
	require.Equal(t, a.Text(), b.Text())
	require.Equal(t, a.Version(), b.Version())
	require.Zero(t, b.Pending(), "duplicates must not pile up in the buffer")

	applied, err := b.Apply(ops[0])
	require.NoError(t, err)
	require.Empty(t, applied)
}

func TestMissing(t *testing.T) {
	a, b := New("A"), New("B")
	ops := typeString(t, a, 0, "abc")
	applyAll(t, b, ops[:2]...)
	x := mustInsert(t, b, 2, 'X')

	require.Equal(t, []Op{ops[2]}, a.Missing(b.Version()))
	require.Equal(t, []Op{x}, b.Missing(a.Version()))
	require.Equal(t, ops, a.Missing(nil))

	applyAll(t, a, b.Missing(a.Version())...)
	applyAll(t, b, a.Missing(b.Version())...)
	require.Equal(t, a.Text(), b.Text())
	require.Empty(t, a.Missing(b.Version()))
}

func TestApplyRejectsMalformedOps(t *testing.T) {
	parent := ID{Clock: 2, Replica: "A"}
	tests := []struct {
		name string
		op   Op
	}{
		{"empty replica", Op{Seq: 1, Kind: KindInsert, Clock: 1, Char: 'x'}},
		{"non-ascii replica", Op{Replica: "é", Seq: 1, Kind: KindInsert, Clock: 1, Char: 'x'}},
		{"seq zero", Op{Replica: "A", Kind: KindInsert, Clock: 1, Char: 'x'}},
		{"unknown kind", Op{Replica: "A", Seq: 1, Kind: "upd"}},
		{"insert clock zero", Op{Replica: "A", Seq: 1, Kind: KindInsert, Char: 'x'}},
		{"insert not above parent", Op{Replica: "B", Seq: 1, Kind: KindInsert, Clock: 1, After: parent, Char: 'x'}},
		{"delete without target", Op{Replica: "A", Seq: 1, Kind: KindDelete}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := New("D")
			_, err := d.Apply(tt.op)
			require.Error(t, err)
			require.Zero(t, d.Pending())
		})
	}
}

// The browser client parses this exact shape (web/rga.js).
func TestOpWireFormat(t *testing.T) {
	ins := Op{Replica: "bob", Seq: 2, Kind: KindInsert, Clock: 7, After: ID{5, "alice"}, Char: 'x'}
	del := Op{Replica: "bob", Seq: 3, Kind: KindDelete, Target: ID{7, "bob"}}
	first := Op{Replica: "bob", Seq: 1, Kind: KindInsert, Clock: 1, Char: 'h'}

	for op, want := range map[Op]string{
		ins:   `{"replica":"bob","seq":2,"kind":"ins","clock":7,"after":{"clock":5,"replica":"alice"},"char":120}`,
		del:   `{"replica":"bob","seq":3,"kind":"del","target":{"clock":7,"replica":"bob"}}`,
		first: `{"replica":"bob","seq":1,"kind":"ins","clock":1,"char":104}`,
	} {
		got, err := json.Marshal(op)
		require.NoError(t, err)
		require.JSONEq(t, want, string(got))

		var back Op
		require.NoError(t, json.Unmarshal([]byte(want), &back))
		require.Equal(t, op, back)
	}
}
