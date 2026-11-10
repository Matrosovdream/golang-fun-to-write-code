package lsmkv

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPutGetDelete(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever})
	_, err := db.Get("nope")
	require.ErrorIs(t, err, ErrNotFound)

	mustPut(t, db, "a", "1")
	require.Equal(t, "1", mustGet(t, db, "a"))
	mustPut(t, db, "a", "2")
	require.Equal(t, "2", mustGet(t, db, "a"))

	mustPut(t, db, "empty", "")
	require.Equal(t, "", mustGet(t, db, "empty"), "an empty value is a value, not a miss")
	require.NoError(t, db.Put("nil", nil))
	require.Equal(t, "", mustGet(t, db, "nil"))

	require.NoError(t, db.Delete("a"))
	_, err = db.Get("a")
	require.ErrorIs(t, err, ErrNotFound)
	require.NoError(t, db.Delete("never-existed"))
	mustPut(t, db, "a", "back")
	require.Equal(t, "back", mustGet(t, db, "a"))
}

func TestValuesAreCopied(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever})
	in := []byte("hello")
	require.NoError(t, db.Put("k", in))
	in[0] = 'J'
	got, err := db.Get("k")
	require.NoError(t, err)
	require.Equal(t, "hello", string(got), "Put must copy its input")
	got[0] = 'J'
	require.Equal(t, "hello", mustGet(t, db, "k"), "Get must copy its output")
}

func TestScanBounds(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, BlockBytes: 64})
	for i := range 30 {
		mustPut(t, db, fmt.Sprintf("k%02d", i), fmt.Sprint(i))
		if i == 9 || i == 19 {
			require.NoError(t, db.Flush()) // spread across two tables and the memtable
		}
	}
	require.NoError(t, db.Delete("k05"))
	require.NoError(t, db.Delete("k25"))

	keys := func(lo, hi string) (ks []string) {
		seq, scanErr := db.Scan(lo, hi)
		for k := range seq {
			ks = append(ks, k)
		}
		require.NoError(t, scanErr())
		return ks
	}
	require.Len(t, keys("", ""), 28)
	require.Equal(t, []string{"k03", "k04", "k06"}, keys("k03", "k07"), "hi is exclusive, deleted keys are skipped")
	require.Equal(t, []string{"k29"}, keys("k29", ""))
	require.Equal(t, []string{"k26"}, keys("k24x", "k27"), "lo need not exist (k25 is deleted)")
	require.Empty(t, keys("k10", "k10"))
	require.Empty(t, keys("k20", "k10"))
	require.Empty(t, keys("zzz", ""))

	n := 0
	seq, scanErr := db.Scan("", "")
	for range seq {
		if n++; n == 3 {
			break
		}
	}
	require.NoError(t, scanErr())
	require.Equal(t, 3, n)
}

func TestReopenRecoversFromWALAndTables(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever})
	mustPut(t, db, "flushed", "1")
	mustPut(t, db, "gone", "x")
	require.NoError(t, db.Flush())
	mustPut(t, db, "walonly", "2")
	require.NoError(t, db.Delete("gone"))
	require.NoError(t, db.Close())

	db = mustOpen(t, dir, Options{})
	require.Equal(t, map[string]string{"flushed": "1", "walonly": "2"}, dump(t, db))
	require.Equal(t, 1, db.Stats().Tables)
}

func TestTombstoneHidesOlderTables(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, CompactAt: 100})
	mustPut(t, db, "k", "v1")
	require.NoError(t, db.Flush())
	mustPut(t, db, "k", "v2")
	require.NoError(t, db.Flush())
	require.NoError(t, db.Delete("k"))
	require.NoError(t, db.Flush()) // the delete now lives only in the newest table
	_, err := db.Get("k")
	require.ErrorIs(t, err, ErrNotFound, "newest table's tombstone must beat the older values")
	require.Empty(t, dump(t, db))
}

func TestCompactMergesAndDropsTombstones(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, CompactAt: 100})
	for round := range 3 {
		for i := range 20 {
			mustPut(t, db, fmt.Sprintf("k%02d", i), fmt.Sprintf("r%d", round))
		}
		require.NoError(t, db.Flush())
	}
	for i := 0; i < 20; i += 2 {
		require.NoError(t, db.Delete(fmt.Sprintf("k%02d", i)))
	}
	require.NoError(t, db.Flush())
	require.Equal(t, 4, db.Stats().Tables)

	before := dump(t, db)
	require.NoError(t, db.Compact())
	require.Equal(t, 1, db.Stats().Tables)
	require.Equal(t, before, dump(t, db))

	// 10 live keys: the 80 overwritten versions and the 10 tombstones are gone.
	require.Len(t, drainAll(db.tables[0].iter("")), 10)

	require.NoError(t, db.Compact(), "nothing to do")
	des, err := filepath.Glob(filepath.Join(db.dir, "*.sst"))
	require.NoError(t, err)
	require.Len(t, des, 1, "inputs are deleted")
}

func TestCompactWhenEverythingIsDeleted(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, CompactAt: 100})
	mustPut(t, db, "a", "1")
	require.NoError(t, db.Flush())
	require.NoError(t, db.Delete("a"))
	require.NoError(t, db.Flush())
	require.NoError(t, db.Compact())
	require.Zero(t, db.Stats().Tables)
	require.Empty(t, dump(t, db))
	require.NoError(t, db.Close())
	db = mustOpen(t, db.dir, Options{})
	require.Empty(t, dump(t, db))
}

func TestAutoCompaction(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, CompactAt: 4})
	for i := range 3 {
		mustPut(t, db, fmt.Sprint("k", i), "v")
		require.NoError(t, db.Flush())
	}
	require.Equal(t, 3, db.Stats().Tables)
	mustPut(t, db, "k3", "v")
	require.NoError(t, db.Flush()) // the fourth table triggers the merge
	require.Equal(t, 1, db.Stats().Tables)
	require.Len(t, dump(t, db), 4)
}

func TestAutoFlushAtMemtableLimit(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, MemtableBytes: 1000, CompactAt: 100})
	for i := range 100 {
		mustPut(t, db, fmt.Sprintf("k%03d", i), "0123456789")
	}
	require.Greater(t, db.Stats().Tables, 3)
	require.Less(t, db.Stats().MemtableBytes, int64(1000))
	require.Len(t, dump(t, db), 100)
}

// A scan that is already running keeps working when the files it reads are
// compacted away, and may call back into the DB.
func TestScanSurvivesCompaction(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever, CompactAt: 100, BlockBytes: 64})
	for round := range 3 {
		for i := range 40 {
			mustPut(t, db, fmt.Sprintf("k%02d", i), fmt.Sprintf("r%d", round))
		}
		require.NoError(t, db.Flush())
	}
	old, err := filepath.Glob(filepath.Join(db.dir, "*.sst"))
	require.NoError(t, err)
	require.Len(t, old, 3)

	n := 0
	seq, scanErr := db.Scan("", "")
	for k, v := range seq {
		if n == 0 {
			require.NoError(t, db.Compact())
			mustPut(t, db, "zz-new", "x")
			_ = mustGet(t, db, k) // a read from inside the loop must not deadlock
		}
		require.Equal(t, "r2", string(v), k)
		n++
	}
	require.NoError(t, scanErr())
	require.GreaterOrEqual(t, n, 40)
	for _, p := range old {
		require.NoFileExists(t, p, "the last reader closing deletes the compacted-away file")
	}
}

func TestClosed(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever})
	require.NoError(t, db.Close())
	require.ErrorIs(t, db.Close(), ErrClosed)
	require.ErrorIs(t, db.Put("a", nil), ErrClosed)
	require.ErrorIs(t, db.Delete("a"), ErrClosed)
	require.ErrorIs(t, db.Flush(), ErrClosed)
	require.ErrorIs(t, db.Compact(), ErrClosed)
	_, err := db.Get("a")
	require.ErrorIs(t, err, ErrClosed)
	seq, scanErr := db.Scan("", "")
	for range seq {
		t.Fatal("closed DB yielded a key")
	}
	require.ErrorIs(t, scanErr(), ErrClosed)
}

func TestSecondOpenIsRefused(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{})
	_, err := Open(dir, Options{})
	require.ErrorIs(t, err, ErrLocked)
	require.ErrorContains(t, err, dir, "the message should name the directory")

	require.NoError(t, db.Close())
	db2, err := Open(dir, Options{})
	require.NoError(t, err, "closing releases the lock")
	require.NoError(t, db2.Close())
}

func TestOpenRefusesMissingManifest(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever})
	mustPut(t, db, "a", "1")
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	require.NoError(t, os.Remove(filepath.Join(dir, "MANIFEST")))

	_, err := Open(dir, Options{})
	require.ErrorIs(t, err, ErrCorrupt, "deleting the tables as orphans would destroy the data")
	require.FileExists(t, filepath.Join(dir, "000002.sst"))
}

func TestOpenRefusesMissingTable(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever})
	mustPut(t, db, "a", "1")
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())
	require.NoError(t, os.Remove(filepath.Join(dir, "000002.sst")))
	_, err := Open(dir, Options{})
	require.ErrorIs(t, err, ErrCorrupt)
}

func TestCorruptTableIsReportedNotMisread(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever, BlockBytes: 64})
	for i := range 50 {
		mustPut(t, db, fmt.Sprintf("k%02d", i), "value")
	}
	require.NoError(t, db.Flush())
	require.NoError(t, db.Close())

	path := filepath.Join(dir, "000002.sst")
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	b[10] ^= 0xff // inside the first data block
	require.NoError(t, os.WriteFile(path, b, 0o644))

	db = mustOpen(t, dir, Options{})
	_, err = db.Get("k00")
	require.ErrorIs(t, err, ErrCorrupt)
	seq, scanErr := db.Scan("", "")
	for range seq {
	}
	require.ErrorIs(t, scanErr(), ErrCorrupt)
	require.Equal(t, "value", mustGet(t, db, "k49"), "other blocks stay readable")
}

func TestWriteFailurePoisonsTheDB(t *testing.T) {
	db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever})
	mustPut(t, db, "a", "1")
	boom := errors.New("disk on fire")
	db.wal.f = failingFile{walFile: db.wal.f, err: boom}
	require.ErrorIs(t, db.Put("b", nil), boom)
	require.ErrorIs(t, db.Put("c", nil), boom, "later writes must not follow a possibly torn record")
	require.Equal(t, "1", mustGet(t, db, "a"), "reads keep working")
}

type failingFile struct {
	walFile
	err error
}

func (f failingFile) Write([]byte) (int, error) { return 0, f.err }

// countingFile counts the fsyncs the WAL asks for.
type countingFile struct {
	walFile
	syncs int
}

func (c *countingFile) Sync() error { c.syncs++; return c.walFile.Sync() }

func TestSyncPolicies(t *testing.T) {
	writes := func(db *DB, n int) {
		for i := range n {
			mustPut(t, db, fmt.Sprint("k", i), "v")
		}
	}
	t.Run("every", func(t *testing.T) {
		db := mustOpen(t, t.TempDir(), Options{Sync: SyncEvery})
		c := &countingFile{walFile: db.wal.f}
		db.wal.f = c
		writes(db, 5)
		require.NoError(t, db.Delete("k0"))
		require.Equal(t, 6, c.syncs, "one fsync per write")
		require.NoError(t, db.Close())
		require.Equal(t, 7, c.syncs, "Close syncs too")
	})
	t.Run("never", func(t *testing.T) {
		db := mustOpen(t, t.TempDir(), Options{Sync: SyncNever})
		c := &countingFile{walFile: db.wal.f}
		db.wal.f = c
		writes(db, 50)
		require.NoError(t, db.Close())
		require.Zero(t, c.syncs)
	})
	t.Run("periodic", func(t *testing.T) {
		dir := t.TempDir()
		synctest.Test(t, func(t *testing.T) { // fake clock: Sleep advances it instantly
			db := mustOpen(t, dir, Options{Sync: SyncPeriodic, SyncPeriod: 50 * time.Millisecond})
			c := &countingFile{walFile: db.wal.f}
			db.wal.f = c
			writes(db, 10)
			require.Zero(t, c.syncs, "all inside the first period")
			time.Sleep(50 * time.Millisecond)
			writes(db, 10)
			require.Equal(t, 1, c.syncs, "first write after the period syncs; the rest ride on it")
			time.Sleep(20 * time.Millisecond)
			writes(db, 10)
			require.Equal(t, 1, c.syncs)
			time.Sleep(30 * time.Millisecond)
			writes(db, 1)
			require.Equal(t, 2, c.syncs)
			require.NoError(t, db.Close())
			require.Equal(t, 3, c.syncs)
		})
	})
}
