package index

import (
	"cmp"
	"slices"
	"testing"
	"testing/quick"

	"github.com/stretchr/testify/require"
)

// Merge-intersect/union/difference against a reference built from maps and a
// scan of the whole document domain: slow, but too simple to be wrong.
func TestMergesMatchNaive(t *testing.T) {
	const domain = 64
	hits := func(docs []uint8, scale float64) []Hit {
		set := map[int]bool{}
		for _, d := range docs {
			set[int(d)%domain] = true
		}
		var out []Hit
		for d := range set {
			out = append(out, Hit{d, float64(d+1) * scale})
		}
		slices.SortFunc(out, func(a, b Hit) int { return cmp.Compare(a.Doc, b.Doc) })
		return out
	}
	naive := func(a, b []Hit, keep func(inA, inB bool) bool) []Hit {
		sa, sb := map[int]float64{}, map[int]float64{}
		for _, h := range a {
			sa[h.Doc] = h.Score
		}
		for _, h := range b {
			sb[h.Doc] = h.Score
		}
		var out []Hit
		for d := range domain {
			_, inA := sa[d]
			_, inB := sb[d]
			if keep(inA, inB) {
				out = append(out, Hit{d, sa[d] + sb[d]})
			}
		}
		return out
	}
	property := func(xs, ys []uint8) bool {
		a, b := hits(xs, 1), hits(ys, 0.5)
		return slices.Equal(intersect(a, b), naive(a, b, func(x, y bool) bool { return x && y })) &&
			slices.Equal(union(a, b), naive(a, b, func(x, y bool) bool { return x || y })) &&
			slices.Equal(difference(a, b), naive(a, b, func(x, y bool) bool { return x && !y }))
	}
	require.NoError(t, quick.Check(property, &quick.Config{MaxCount: 2000}))
}
