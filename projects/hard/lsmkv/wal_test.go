package lsmkv

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

type memFile struct {
	bytes.Buffer
	syncs int
}

func (m *memFile) Sync() error  { m.syncs++; return nil }
func (m *memFile) Close() error { return nil }

func logOf(t *testing.T, es ...entry) []byte {
	f := &memFile{}
	w := &walWriter{f}
	for _, e := range es {
		require.NoError(t, w.append(e))
	}
	return f.Bytes()
}

func replay(t *testing.T, data []byte) ([]entry, int, error) {
	var got []entry
	n, err := decodeWAL(data, func(e entry) { got = append(got, e) })
	return got, n, err
}

func TestWALRoundTrip(t *testing.T) {
	want := []entry{put("a", "1"), del("a"), put("", ""), put("big", string(make([]byte, 5000)))}
	got, n, err := replay(t, logOf(t, want...))
	require.NoError(t, err)
	require.Len(t, got, len(want))
	for i := range want {
		require.Equal(t, want[i].key, got[i].key)
		require.Equal(t, want[i].tomb, got[i].tomb)
		require.True(t, bytes.Equal(want[i].val, got[i].val))
	}
	require.Equal(t, len(logOf(t, want...)), n)
}

func TestWALTornTailIsDropped(t *testing.T) {
	log := logOf(t, put("a", "1"), put("b", "2"))
	one := len(logOf(t, put("a", "1")))
	for cut := one; cut < len(log); cut++ {
		got, n, err := replay(t, log[:cut])
		require.NoError(t, err, "cut at %d", cut)
		require.Len(t, got, 1)
		require.Equal(t, one, n, "cut at %d", cut)
	}
}

func TestWALBadChecksum(t *testing.T) {
	log := logOf(t, put("a", "1"), put("b", "2"), put("c", "3"))
	one := len(logOf(t, put("a", "1")))

	mid := bytes.Clone(log)
	mid[one+len(mid[one:])/3] ^= 0xff // inside the second record, a third follows
	got, n, err := replay(t, mid)
	require.ErrorIs(t, err, ErrCorrupt)
	require.Len(t, got, 1, "the intact prefix is still delivered")
	require.Equal(t, one, n)

	last := bytes.Clone(log)
	last[len(last)-1] ^= 0xff // in the final record: indistinguishable from a torn write
	got, n, err = replay(t, last)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, 2*one, n)
}

func TestWALGarbageLength(t *testing.T) {
	_, _, err := replay(t, bytes.Repeat([]byte{0xff}, 12)) // varint that never ends
	require.ErrorIs(t, err, ErrCorrupt)
	got, n, err := replay(t, []byte{0xff, 0xff}) // varint cut short: torn
	require.NoError(t, err)
	require.Empty(t, got)
	require.Zero(t, n)
}
