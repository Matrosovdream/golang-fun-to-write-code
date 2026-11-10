package lsmkv

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func hashes(prefix string, n int) []uint64 {
	hs := make([]uint64, n)
	for i := range hs {
		hs[i] = hashKey(fmt.Sprintf("%s%d", prefix, i))
	}
	return hs
}

func TestBloomNoFalseNegativesAndFalsePositiveRate(t *testing.T) {
	for _, n := range []int{1, 10, 1000, 20000} {
		present := hashes("present-", n)
		b := buildBloom(present, 10)
		for _, h := range present {
			require.True(t, b.mayContain(h), "false negative with %d keys", n)
		}
		const probes = 100000
		fp := 0
		for _, h := range hashes("absent-", probes) {
			if b.mayContain(h) {
				fp++
			}
		}
		rate := float64(fp) / probes
		t.Logf("n=%-6d filter=%dB false-positive rate %.2f%%", n, len(b), rate*100)
		require.Less(t, rate, 0.03, "10 bits/key should land near 1%%; the spec bound is 3%%")
	}
}

func TestBloomDisabledAndUnreadableMeanMaybe(t *testing.T) {
	h := hashKey("anything")
	require.Nil(t, buildBloom(hashes("a", 10), 0))
	require.Nil(t, buildBloom(hashes("a", 10), -1))
	require.Nil(t, buildBloom(nil, 10))
	for _, b := range []bloom{nil, {}, {7}, {0, 0, 0}, {0, 0, 0xff}} { // empty, too short, k=0, k too big
		require.True(t, b.mayContain(h))
	}
}

// The filter bits are persisted, so the hash may never change. maphash
// would pass every other test here and still corrupt every table on restart.
func TestHashKeyIsStable(t *testing.T) {
	// FNV-1a("hello") is 0xa430d84680aabd0b; this is that value after the finalizer.
	require.Equal(t, uint64(0xc8c76d8999b8ebcc), hashKey("hello"))
	require.Equal(t, hashKey("hello"), hashKey("hello"))
	require.NotEqual(t, hashKey("hello"), hashKey("hellp"))
}
