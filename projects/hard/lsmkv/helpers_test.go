package lsmkv

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func mustOpen(t testing.TB, dir string, opts Options) *DB {
	t.Helper()
	db, err := Open(dir, opts)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() }) // a second Close is a harmless ErrClosed
	return db
}

func mustGet(t testing.TB, db *DB, key string) string {
	t.Helper()
	v, err := db.Get(key)
	require.NoError(t, err, "get %q", key)
	return string(v)
}

func mustPut(t testing.TB, db *DB, key, val string) {
	t.Helper()
	require.NoError(t, db.Put(key, []byte(val)))
}

// dump reads the whole DB through Scan.
func dump(t testing.TB, db *DB) map[string]string {
	t.Helper()
	got := map[string]string{}
	seq, scanErr := db.Scan("", "")
	for k, v := range seq {
		_, dup := got[k]
		require.False(t, dup, "scan returned %q twice", k)
		got[k] = string(v)
	}
	require.NoError(t, scanErr())
	return got
}

func readFiles(t testing.TB, dir string) map[string][]byte {
	t.Helper()
	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	files := map[string][]byte{}
	for _, de := range des {
		if de.Name() == "LOCK" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, de.Name()))
		require.NoError(t, err)
		files[de.Name()] = b
	}
	return files
}

// restore lays files out in a fresh directory, as a crash would have left them.
func restore(t testing.TB, files map[string][]byte) string {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), b, 0o644))
	}
	return dir
}

func with(files map[string][]byte, extra map[string][]byte) map[string][]byte {
	out := maps.Clone(files)
	maps.Copy(out, extra)
	return out
}

func without(files map[string][]byte, names ...string) map[string][]byte {
	out := maps.Clone(files)
	for _, n := range names {
		delete(out, n)
	}
	return out
}

// sliceSource is a source over a fixed slice, optionally failing at the end.
type sliceSource struct {
	es   []entry
	fail error
}

func (s *sliceSource) next() (entry, bool) {
	if len(s.es) == 0 {
		return entry{}, false
	}
	e := s.es[0]
	s.es = s.es[1:]
	return e, true
}

func (s *sliceSource) err() error { return s.fail }

func put(k, v string) entry { return entry{key: k, val: []byte(v)} }
func del(k string) entry    { return entry{key: k, tomb: true} }
