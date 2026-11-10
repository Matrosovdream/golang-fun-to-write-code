package lsmkv

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func encodeAll(es []entry) []byte {
	var b []byte
	for _, e := range es {
		b = appendEntry(b, e)
	}
	return b
}

func TestBlockRoundTrip(t *testing.T) {
	es := []entry{put("", ""), put("a", "1"), del("b"), put("c", "a longer value"), put("\x00\xff", "bin\x00ary")}
	got, err := decodeBlock(encodeAll(es))
	require.NoError(t, err)
	require.Len(t, got, len(es))
	for i := range es {
		require.Equal(t, es[i].key, got[i].key)
		require.Equal(t, es[i].tomb, got[i].tomb)
		require.True(t, bytes.Equal(es[i].val, got[i].val))
	}
}

func TestDecodeValueCannotOverwriteNeighbour(t *testing.T) {
	got, err := decodeBlock(encodeAll([]entry{put("a", "1"), put("b", "2")}))
	require.NoError(t, err)
	_ = append(got[0].val, 'X', 'X', 'X')
	require.Equal(t, "b", got[1].key)
	require.Equal(t, "2", string(got[1].val))
}

func TestDecodeRejectsMalformed(t *testing.T) {
	good := encodeAll([]entry{put("key", "value")})
	tests := []struct {
		name string
		in   []byte
	}{
		{"truncated value", good[:len(good)-1]},
		{"truncated key", good[:4]},
		{"only a kind", good[:1]},
		{"unknown kind", append([]byte{9}, good[1:]...)},
		{"tombstone with a value", append([]byte{kindDelete}, good[1:]...)},
		{"huge key length", []byte{0, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x7f, 0}},
		{"unterminated varint", []byte{0, 0x80, 0x80}},
		{"lengths overflow when added", []byte{0, 0x7f, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}},
	}
	for _, tt := range tests {
		_, err := decodeBlock(tt.in)
		require.ErrorIs(t, err, ErrCorrupt, tt.name)
	}
}

func TestChecksumFraming(t *testing.T) {
	framed := appendCRC([]byte("payload"))
	got, err := stripCRC(framed)
	require.NoError(t, err)
	require.Equal(t, "payload", string(got))

	for i := range framed {
		bad := bytes.Clone(framed)
		bad[i] ^= 0x01
		_, err := stripCRC(bad)
		require.ErrorIs(t, err, ErrCorrupt, "flip at %d", i)
	}
	_, err = stripCRC([]byte{1, 2, 3})
	require.ErrorIs(t, err, ErrCorrupt)
}

// FuzzDecodeBlock: arbitrary bytes may be rejected but must never panic (the
// block, checksum, handle, manifest and WAL decoders all get the same input),
// and whatever decodes as a block must survive being re-encoded and decoded again.
func FuzzDecodeBlock(f *testing.F) {
	f.Add(encodeAll([]entry{put("a", "1"), del("b"), put("", "")}))
	f.Add([]byte{})
	f.Add([]byte{0, 1, 1, 'k', 'v'})
	f.Add([]byte{1, 0, 5})
	f.Add(appendCRC(encodeAll([]entry{put("k", "v")})))
	f.Fuzz(func(t *testing.T, data []byte) {
		es, err := decodeBlock(data)
		_, _ = stripCRC(data)
		_, _ = decodeHandle(data)
		_, _ = decodeManifest(data)
		valid, _ := decodeWAL(data, func(entry) {})
		require.LessOrEqual(t, valid, len(data))
		if err != nil {
			require.True(t, errors.Is(err, ErrCorrupt))
			return
		}
		again, err := decodeBlock(encodeAll(es))
		require.NoError(t, err)
		require.Equal(t, len(es), len(again))
		for i := range es {
			require.Equal(t, es[i].key, again[i].key)
			require.Equal(t, es[i].tomb, again[i].tomb)
			require.True(t, bytes.Equal(es[i].val, again[i].val))
		}
	})
}
