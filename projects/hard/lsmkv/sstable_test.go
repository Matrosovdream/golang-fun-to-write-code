package lsmkv

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// buildTable writes es (ascending) as table 1 in a fresh dir and opens it.
func buildTable(t *testing.T, es []entry, blockBytes, bloomBits int) *table {
	t.Helper()
	path := filepath.Join(t.TempDir(), "000001.sst")
	f, err := os.Create(path)
	require.NoError(t, err)
	w := newTableWriter(f, blockBytes, bloomBits)
	for _, e := range es {
		require.NoError(t, w.add(e))
	}
	require.NoError(t, w.finish())
	require.NoError(t, f.Close())
	tb, err := openTable(path, 1)
	require.NoError(t, err)
	t.Cleanup(tb.unref)
	return tb
}

func numbered(n int) []entry {
	es := make([]entry, n)
	for i := range es {
		es[i] = put(fmt.Sprintf("key%04d", i*2), fmt.Sprintf("value-%d", i)) // even keys only
		if i%7 == 3 {
			es[i] = del(es[i].key)
		}
	}
	return es
}

func TestTableGet(t *testing.T) {
	es := numbered(500)
	tb := buildTable(t, es, 200, 10)
	require.Greater(t, len(tb.index), 20, "small blocks should give many index entries")

	for _, want := range es {
		got, ok, err := tb.get(want.key, hashKey(want.key))
		require.NoError(t, err)
		require.True(t, ok, want.key)
		require.Equal(t, want.tomb, got.tomb, "tombstones are stored, not dropped: %s", want.key)
		require.Equal(t, string(want.val), string(got.val))
	}
	for _, miss := range []string{"", "a", "key0001", "key0999", "key9999", "zzz"} {
		_, ok, err := tb.get(miss, hashKey(miss))
		require.NoError(t, err)
		require.False(t, ok, miss)
	}
}

func TestTableBloomSkipsAbsentKeys(t *testing.T) {
	es := numbered(500)
	with := buildTable(t, es, 200, 10)
	without := buildTable(t, es, 200, -1)
	require.NotEmpty(t, with.bloom)
	require.Empty(t, without.bloom)

	skipped := 0
	for i := range 1000 {
		k := fmt.Sprintf("key%04d", i*2+1) // odd: never present
		if !with.bloom.mayContain(hashKey(k)) {
			skipped++
		}
		_, ok, err := without.get(k, hashKey(k))
		require.NoError(t, err)
		require.False(t, ok)
	}
	require.Greater(t, skipped, 950, "a 10-bit filter should rule out ~99%% of absent keys")
}

func TestTableIter(t *testing.T) {
	es := numbered(300)
	tb := buildTable(t, es, 150, 10)

	all := drainAll(tb.iter(""))
	require.Len(t, all, len(es))
	for i := range es {
		require.Equal(t, es[i].key, all[i].key)
	}

	for _, lo := range []string{"key0000", "key0001", "key0300", "key0301", "key0598", "key0599", "zzz"} {
		var want []string
		for _, e := range es {
			if e.key >= lo {
				want = append(want, e.key)
			}
		}
		var got []string
		for _, e := range drainAll(tb.iter(lo)) {
			got = append(got, e.key)
		}
		require.Equal(t, want, got, "lo=%q", lo)
	}
}

func TestTableWriterRejectsUnsortedKeys(t *testing.T) {
	w := newTableWriter(&memFile{}, 100, 10)
	require.NoError(t, w.add(put("b", "")))
	require.Error(t, w.add(put("a", "")))
	require.Error(t, w.add(put("b", "")))
}

func TestTableDetectsCorruption(t *testing.T) {
	tb := buildTable(t, numbered(200), 200, 10)
	good, err := os.ReadFile(tb.path)
	require.NoError(t, err)

	open := func(b []byte) (*table, error) {
		p := filepath.Join(t.TempDir(), "000001.sst")
		require.NoError(t, os.WriteFile(p, b, 0o644))
		return openTable(p, 1)
	}

	t.Run("every footer byte", func(t *testing.T) {
		for i := len(good) - footerSize; i < len(good); i++ {
			bad := append([]byte(nil), good...)
			bad[i] ^= 0x10
			if tb2, err := open(bad); err == nil { // a flip may leave a still-valid, different table
				tb2.unref()
				t.Fatalf("footer byte %d flipped and Open still succeeded", i-(len(good)-footerSize))
			} else {
				require.ErrorIs(t, err, ErrCorrupt, "footer byte %d", i-(len(good)-footerSize))
			}
		}
	})
	t.Run("truncated", func(t *testing.T) {
		for _, n := range []int{0, 10, footerSize - 1, len(good) - 1, len(good) / 2} {
			_, err := open(good[:n])
			require.ErrorIs(t, err, ErrCorrupt, "len %d", n)
		}
	})
	t.Run("a data byte never yields wrong data", func(t *testing.T) {
		clean := drainAll(tb.iter(""))
		for i := 0; i < int(tb.index[len(tb.index)-1].h.off+tb.index[len(tb.index)-1].h.size); i += 13 {
			bad := append([]byte(nil), good...)
			bad[i] ^= 0x04
			tb2, err := open(bad)
			require.NoError(t, err) // data blocks are only verified when read
			it := tb2.iter("")
			got := drainAll(it)
			require.ErrorIs(t, it.err(), ErrCorrupt, "flip at byte %d went unnoticed", i)
			require.Less(t, len(got), len(clean))
			for j, e := range got { // what was returned before the bad block is still right
				require.Equal(t, clean[j].key, e.key)
			}
			tb2.unref()
		}
	})
}
