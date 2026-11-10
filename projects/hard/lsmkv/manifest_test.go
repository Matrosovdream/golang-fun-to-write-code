package lsmkv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManifestRoundTrip(t *testing.T) {
	for _, m := range []manifest{{Wal: 1, Next: 2}, {Wal: 7, Next: 12, Tables: []uint64{3, 5, 9}}} {
		got, err := decodeManifest(m.encode())
		require.NoError(t, err)
		require.Equal(t, m, got)
	}
}

func TestManifestRejectsGarbage(t *testing.T) {
	for _, in := range []string{
		"", "hello", "{}", "[]", `{"wal":0,"next":2}`, `{"wal":5,"next":5}`, `{"wal":-1,"next":2}`,
		`{"wal":1,"next":2,"tables":[1,"x"]}`, `{"wal":1,"next":2`, `{"wal":99999999999999999999999,"next":2}`,
	} {
		_, err := decodeManifest([]byte(in))
		require.ErrorIs(t, err, ErrCorrupt, "%q", in)
	}
}

func TestManifestWriteIsAtomicReplace(t *testing.T) {
	dir := t.TempDir()
	_, ok, err := readManifest(dir)
	require.NoError(t, err)
	require.False(t, ok)

	require.NoError(t, writeManifest(dir, manifest{Wal: 1, Next: 2}))
	require.NoError(t, writeManifest(dir, manifest{Wal: 3, Next: 4, Tables: []uint64{2}}))
	m, ok, err := readManifest(dir)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, uint64(3), m.Wal)
	require.Equal(t, []uint64{2}, m.Tables)
	require.NoFileExists(t, filepath.Join(dir, "MANIFEST.tmp"))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "MANIFEST"), []byte("junk"), 0o644))
	_, _, err = readManifest(dir)
	require.ErrorIs(t, err, ErrCorrupt)
}
