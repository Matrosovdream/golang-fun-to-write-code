package index

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSnapshotRoundTrip(t *testing.T) {
	ix := corpusIndex(t)
	path := filepath.Join(t.TempDir(), "idx")
	require.NoError(t, ix.Save(path))

	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, ix.Stats(), loaded.Stats())
	for _, q := range []string{`goroutine AND "memory model" -mutex`, "channel OR select", `"happens before"`, "-race"} {
		require.Equal(t, search(t, ix, q), search(t, loaded, q), q)
	}
	// The loaded index is a live one: it accepts documents and rejects duplicates.
	_, err = loaded.Add("new.md", "freshly added needle")
	require.NoError(t, err)
	_, err = loaded.Add("new.md", "again")
	require.ErrorIs(t, err, ErrExists)
	require.Equal(t, []string{"new.md"}, names(search(t, loaded, "needle")))
}

func TestSnapshotOfEmptyIndexIsUsable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "idx")
	require.NoError(t, New().Save(path))
	ix, err := Load(path)
	require.NoError(t, err)
	_, err = ix.Add("a.md", "hello world") // would panic on a nil postings map
	require.NoError(t, err)
	require.Equal(t, []string{"a.md"}, names(search(t, ix, "hello")))
}

func TestLoadErrors(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(filepath.Join(dir, "missing"))
	require.ErrorIs(t, err, fs.ErrNotExist)

	good := filepath.Join(dir, "good")
	require.NoError(t, corpusIndex(t).Save(good))
	data, err := os.ReadFile(good)
	require.NoError(t, err)
	torn := filepath.Join(dir, "torn")
	require.NoError(t, os.WriteFile(torn, data[:len(data)/2], 0o600))
	_, err = Load(torn)
	require.Error(t, err)
}

// A snapshot that dies half way must never replace the good one.
func TestHalfWrittenSnapshotNeverReplacesGoodOne(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "idx")
	require.NoError(t, corpusIndex(t).Save(path))
	good, err := os.ReadFile(path)
	require.NoError(t, err)
	intact := func() {
		t.Helper()
		now, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, good, now, "snapshot changed")
		_, err = Load(path)
		require.NoError(t, err)
	}

	// The writer fails after emitting half the bytes: error out, clean up.
	disk := errors.New("disk full")
	err = writeAtomic(path, func(w io.Writer) error {
		_, _ = w.Write(good[:len(good)/2])
		return disk
	})
	require.ErrorIs(t, err, disk)
	intact()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "temp file left behind")

	// The process dies mid-write (a panic stands in for a crash, so no
	// cleanup runs): the temp file is garbage, but the real name is untouched.
	require.Panics(t, func() {
		_ = writeAtomic(path, func(w io.Writer) error {
			_, _ = w.Write(good[:10])
			panic("power cut")
		})
	})
	intact()

	// And an interrupted save does not poison the next one.
	ix := corpusIndex(t)
	_, err = ix.Add("late.md", "late arrival")
	require.NoError(t, err)
	require.NoError(t, ix.Save(path))
	loaded, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, []string{"late.md"}, names(search(t, loaded, "late")))
}
