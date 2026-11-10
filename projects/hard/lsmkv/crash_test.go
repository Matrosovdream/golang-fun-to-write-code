package lsmkv

import (
	"encoding/binary"
	"fmt"
	"maps"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// walScenario writes 50 random puts and deletes into a fresh DB, closes it
// (the WAL is the only copy of the data) and returns the WAL bytes, the model
// after each prefix of writes, and the WAL size after each write.
type walScenario struct {
	files  map[string][]byte
	wal    []byte
	states []map[string]string // states[k] = model after the first k writes
	bounds []int               // bounds[k] = WAL length once write k+1 is in
}

func newWALScenario(t *testing.T) walScenario {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever})
	rng := rand.New(rand.NewPCG(7, 7))
	model := map[string]string{}
	sc := walScenario{states: []map[string]string{{}}}
	for i := range 50 {
		k := fmt.Sprintf("k%d", rng.IntN(8))
		if rng.IntN(4) == 0 {
			require.NoError(t, db.Delete(k))
			delete(model, k)
		} else {
			v := fmt.Sprintf("v%d", i)
			mustPut(t, db, k, v)
			model[k] = v
		}
		sc.states = append(sc.states, maps.Clone(model))
		st, err := os.Stat(walPath(dir, 1))
		require.NoError(t, err)
		sc.bounds = append(sc.bounds, int(st.Size()))
	}
	require.NoError(t, db.Close())
	sc.files = readFiles(t, dir)
	sc.wal = sc.files["000001.wal"]
	require.Equal(t, sc.bounds[49], len(sc.wal))
	return sc
}

// intact is how many whole records fit in the first n bytes of the WAL.
func (sc walScenario) intact(n int) int {
	k := 0
	for k < len(sc.bounds) && sc.bounds[k] <= n {
		k++
	}
	return k
}

// TestCrashTruncatedWAL cuts the log at every byte offset, as a crash in the
// middle of an append would, and demands the recovered state be exactly the
// model after the writes that were completely on disk: never a torn or
// reordered state, and the log must stay appendable afterwards.
func TestCrashTruncatedWAL(t *testing.T) {
	sc := newWALScenario(t)
	for off := 0; off <= len(sc.wal); off++ {
		dir := restore(t, with(sc.files, map[string][]byte{"000001.wal": sc.wal[:off]}))
		want := sc.states[sc.intact(off)]

		db, err := Open(dir, Options{Sync: SyncNever})
		require.NoError(t, err, "offset %d", off)
		require.Equal(t, want, dump(t, db), "offset %d: recovered state is not the model after %d writes", off, sc.intact(off))

		if off%4 != 0 && !slices.Contains(sc.bounds, off) && !slices.Contains(sc.bounds, off-1) {
			require.NoError(t, db.Close())
			continue // the append check below costs a second open; sample it
		}
		mustPut(t, db, "after", "crash") // must not land behind a torn tail
		require.NoError(t, db.Close())
		db, err = Open(dir, Options{Sync: SyncNever})
		require.NoError(t, err, "offset %d after append", off)
		require.Equal(t, with2(want, "after", "crash"), dump(t, db), "offset %d after append", off)
		require.NoError(t, db.Close())
	}
}

func with2(m map[string]string, k, v string) map[string]string {
	m = maps.Clone(m)
	m[k] = v
	return m
}

// TestCrashFlippedByte flips one bit at every offset of the log. The only
// acceptable outcomes are an ErrCorrupt from Open, or the state after the
// writes preceding the damaged record (the damaged record is treated as the
// torn tail). Damage inside a record that is followed by more records can
// never be mistaken for a torn tail, so there Open must refuse.
func TestCrashFlippedByte(t *testing.T) {
	sc := newWALScenario(t)
	var refused, dropped int
	for i := range sc.wal {
		rec := sc.intact(i) // the record containing byte i is record rec
		start := 0
		if rec > 0 {
			start = sc.bounds[rec-1]
		}
		_, lenBytes := binary.Uvarint(sc.wal[start:])
		inBody := i >= start+lenBytes
		isLast := rec == len(sc.bounds)-1

		flipped := append([]byte(nil), sc.wal...)
		flipped[i] ^= 1 << (i % 8)
		dir := restore(t, with(sc.files, map[string][]byte{"000001.wal": flipped}))

		db, err := Open(dir, Options{Sync: SyncNever})
		switch {
		case err != nil:
			require.ErrorIs(t, err, ErrCorrupt, "offset %d", i)
			refused++
		case !isLast && inBody:
			t.Fatalf("offset %d (record %d body, not the last record): Open accepted a corrupt log", i, rec)
		default:
			require.Equal(t, sc.states[rec], dump(t, db), "offset %d: not the state before damaged record %d", i, rec)
			require.NoError(t, db.Close())
			dropped++
		}
	}
	t.Logf("%d flips refused with ErrCorrupt, %d recovered by dropping the damaged tail", refused, dropped)
	require.Positive(t, refused)
	require.Positive(t, dropped)
}

// liveDir asserts that after Open the directory holds exactly the files the
// manifest vouches for: the crash leftovers are gone.
func liveDir(t *testing.T, dir string) {
	t.Helper()
	m, ok, err := readManifest(dir)
	require.NoError(t, err)
	require.True(t, ok)
	want := []string{"LOCK", "MANIFEST", filepath.Base(walPath(dir, m.Wal))}
	for _, n := range m.Tables {
		want = append(want, filepath.Base(sstPath(dir, n)))
	}
	var got []string
	des, err := os.ReadDir(dir)
	require.NoError(t, err)
	for _, de := range des {
		got = append(got, de.Name())
	}
	require.ElementsMatch(t, want, got)
}

func checkRecovery(t *testing.T, cases map[string]map[string][]byte, model map[string]string) {
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			dir := restore(t, files)
			db, err := Open(dir, Options{Sync: SyncNever})
			require.NoError(t, err)
			require.Equal(t, model, dump(t, db))
			liveDir(t, dir)

			mustPut(t, db, "later", "x") // the recovered DB is fully usable
			require.NoError(t, db.Flush())
			require.Equal(t, with2(model, "later", "x"), dump(t, db))
			require.NoError(t, db.Close())
			db, err = Open(dir, Options{Sync: SyncNever})
			require.NoError(t, err)
			require.Equal(t, with2(model, "later", "x"), dump(t, db))
		})
	}
}

// TestCrashDuringFlush builds the on-disk state a crash would leave after
// each step of a flush by combining the files from just before and just
// after it, and checks that Open recovers every acknowledged write.
func TestCrashDuringFlush(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever})
	model := map[string]string{}
	for i := range 12 {
		k := fmt.Sprintf("k%02d", i)
		mustPut(t, db, k, "v"+k)
		model[k] = "v" + k
	}
	require.NoError(t, db.Delete("k03"))
	delete(model, "k03")
	before := readFiles(t, dir) // MANIFEST (wal 1), 000001.wal
	require.NoError(t, db.Flush())
	after := readFiles(t, dir) // MANIFEST (wal 3, table 2), 000002.sst, 000003.wal
	require.NoError(t, db.Close())
	require.Contains(t, after, "000002.sst")
	require.NotContains(t, after, "000001.wal")

	checkRecovery(t, map[string]map[string][]byte{
		"died while writing the table":    with(before, map[string][]byte{"000002.sst.tmp": []byte("half a tab")}),
		"table renamed, manifest not yet": with(before, map[string][]byte{"000002.sst": after["000002.sst"]}),
		"manifest committed, no new wal":  without(after, "000003.wal"),
		"manifest committed, old wal left": with(after, map[string][]byte{
			"000001.wal": before["000001.wal"]}),
		"manifest tmp left behind": with(after, map[string][]byte{"MANIFEST.tmp": []byte("junk")}),
		"finished":                 after,
	}, model)
}

// TestCrashDuringCompaction is the same exercise for a compaction, whose
// commit point is again the manifest rename.
func TestCrashDuringCompaction(t *testing.T) {
	dir := t.TempDir()
	db := mustOpen(t, dir, Options{Sync: SyncNever, CompactAt: 100})
	model := map[string]string{}
	for round := range 3 {
		for i := range 6 {
			k := fmt.Sprintf("k%d", i*2+round%2) // overlapping key sets
			mustPut(t, db, k, fmt.Sprintf("r%d", round))
			model[k] = fmt.Sprintf("r%d", round)
		}
		if round == 2 {
			require.NoError(t, db.Delete("k1"))
			delete(model, "k1")
		}
		require.NoError(t, db.Flush())
	}
	before := readFiles(t, dir) // tables 2, 4, 6
	require.NoError(t, db.Compact())
	after := readFiles(t, dir) // one table, 8
	require.NoError(t, db.Close())
	require.Contains(t, after, "000008.sst")
	require.NotContains(t, after, "000002.sst")

	inputs := map[string][]byte{}
	for _, n := range []string{"000002.sst", "000004.sst", "000006.sst"} {
		inputs[n] = before[n]
	}
	checkRecovery(t, map[string]map[string][]byte{
		"died while writing the merged table": with(before, map[string][]byte{"000008.sst.tmp": []byte("partial")}),
		"merged table renamed, no commit":     with(before, map[string][]byte{"000008.sst": after["000008.sst"]}),
		"committed, inputs not deleted":       with(after, inputs),
		"finished":                            after,
	}, model)
}
