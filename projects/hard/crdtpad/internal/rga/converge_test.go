package rga

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// simulate is the heart of the test suite. 3-5 replicas make random local
// edits; every edit is queued for every other replica, sometimes twice, and
// deliveries to each replica pick a random queued op, so each replica sees
// its own shuffled, duplicated interleaving mixed with its own further edits.
// Afterwards every replica must hold the same text, nothing may be buffered,
// the characters present must be exactly the inserted-and-not-deleted ones,
// and every insert must still sit between the neighbours its author saw.
//
// The whole run is a function of seed. Replay one with:
//
//	go test -run 'TestConvergence/seed=17$' ./internal/rga
func simulate(t *testing.T, seed uint64) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("replay: go test -run 'TestConvergence/seed=%d$' ./internal/rga", seed)
		}
	})
	rng := rand.New(rand.NewPCG(seed, 0))

	n := 3 + rng.IntN(3)
	docs := make([]*Doc, n)
	inbox := make([][]Op, n)
	for i := range docs {
		docs[i] = New(fmt.Sprintf("r%d", i))
	}
	var all []Op
	var intents []intent

	deliverOne := func(i int) {
		k := rng.IntN(len(inbox[i]))
		op := inbox[i][k]
		inbox[i][k] = inbox[i][len(inbox[i])-1]
		inbox[i] = inbox[i][:len(inbox[i])-1]
		_, err := docs[i].Apply(op)
		require.NoError(t, err, "seed %d", seed)
	}

	steps := 100 + rng.IntN(300)
	pDeliver := 20 + rng.IntN(70) // low: replicas edit blind; high: nearly in sync
	for range steps {
		i := rng.IntN(n)
		if len(inbox[i]) > 0 && rng.IntN(100) < pDeliver {
			deliverOne(i)
			continue
		}
		op, right := randomEdit(t, rng, docs[i])
		all = append(all, op)
		if op.Kind == KindInsert {
			intents = append(intents, intent{op.ID(), op.After, right})
		}
		for j := range docs {
			if j == i {
				continue
			}
			inbox[j] = append(inbox[j], op)
			if rng.IntN(4) == 0 {
				inbox[j] = append(inbox[j], op)
			}
		}
	}
	for i := range docs {
		for len(inbox[i]) > 0 {
			deliverOne(i)
		}
	}

	want := docs[0].Text()
	for i, d := range docs {
		require.Equal(t, want, d.Text(), "seed %d: replica %d diverged from replica 0", seed, i)
		require.Zero(t, d.Pending(), "seed %d: replica %d still buffers ops", seed, i)
		require.Equal(t, docs[0].Version(), d.Version(), "seed %d: replica %d version", seed, i)
	}
	require.Equal(t, survivors(all), sortedRunes(want), "seed %d: characters lost or resurrected", seed)

	at := docs[0].positions()
	for _, in := range intents {
		require.Less(t, at[in.left], at[in.id], "seed %d: %v moved before its left neighbour", seed, in.id)
		if in.right != (ID{}) {
			require.Less(t, at[in.id], at[in.right], "seed %d: %v moved after its right neighbour", seed, in.id)
		}
	}
}

// intent records where an author put a character: after left and before right
// (zero if it went at the end). Nodes never move once placed, so the order
// must hold forever, tombstones included.
type intent struct{ id, left, right ID }

func (d *Doc) positions() map[ID]int {
	at := map[ID]int{}
	i := 0
	for n := d.head; n != nil; n = n.next {
		at[n.id] = i
		i++
	}
	return at
}

// randomEdit mixes uniform positions with positions clustered at the start,
// where concurrent inserts collide most often. For an insert it also returns
// the ID of the character that was at the position before (zero at the end of
// the text): the right-hand neighbour the author saw.
func randomEdit(t *testing.T, rng *rand.Rand, d *Doc) (Op, ID) {
	t.Helper()
	n := d.Len()
	if n > 0 && rng.IntN(100) < 30 {
		return mustDelete(t, d, rng.IntN(n)), ID{}
	}
	pos := rng.IntN(n + 1)
	if rng.IntN(2) == 0 {
		pos = rng.IntN(min(n, 2) + 1)
	}
	var right ID
	if next := d.visible(pos); next != nil {
		right = next.id
	}
	return mustInsert(t, d, pos, rune('a'+rng.IntN(26))), right
}

// survivors is the ground truth: sorted runes of every insert no delete targets.
func survivors(ops []Op) []rune {
	deleted := map[ID]bool{}
	for _, op := range ops {
		if op.Kind == KindDelete {
			deleted[op.Target] = true
		}
	}
	var out []rune
	for _, op := range ops {
		if op.Kind == KindInsert && !deleted[op.ID()] {
			out = append(out, op.Char)
		}
	}
	slices.Sort(out)
	return out
}

func sortedRunes(s string) []rune {
	r := []rune(s)
	slices.Sort(r)
	return r
}

func TestConvergence(t *testing.T) {
	for seed := uint64(1); seed <= 300; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) { simulate(t, seed) })
	}
}

func FuzzConvergence(f *testing.F) {
	for _, seed := range []uint64{1, 2, 3} {
		f.Add(seed)
	}
	f.Fuzz(simulate)
}
