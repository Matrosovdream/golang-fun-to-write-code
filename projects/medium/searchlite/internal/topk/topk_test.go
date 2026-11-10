package topk

import (
	"cmp"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTopK(t *testing.T) {
	tests := []struct {
		name string
		k    int
		in   []int
		want []int
	}{
		{"fewer than k", 5, []int{3, 1, 2}, []int{3, 2, 1}},
		{"exactly k", 3, []int{3, 1, 2}, []int{3, 2, 1}},
		{"more than k", 2, []int{5, 9, 1, 7, 3}, []int{9, 7}},
		{"duplicates", 3, []int{4, 4, 4, 4, 1}, []int{4, 4, 4}},
		{"k is zero", 0, []int{1, 2}, []int{}},
		{"k is negative", -1, []int{1, 2}, []int{}},
		{"empty", 3, nil, []int{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tk := New(tt.k, func(a, b int) bool { return a < b })
			for _, x := range tt.in {
				tk.Push(x)
			}
			require.Equal(t, tt.want, tk.Drain())
		})
	}
}

// Against the obvious O(n log n) answer: sort everything, take the first k.
func TestTopKMatchesSort(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for range 200 {
		in := make([]int, rng.IntN(60))
		for i := range in {
			in[i] = rng.IntN(20)
		}
		k := rng.IntN(15)
		tk := New(k, func(a, b int) bool { return a < b })
		for _, x := range in {
			tk.Push(x)
		}
		want := slices.SortedFunc(slices.Values(in), func(a, b int) int { return cmp.Compare(b, a) })
		want = want[:min(k, len(want))]
		require.Truef(t, slices.Equal(want, tk.Drain()), "k=%d in=%v", k, in)
	}
}
