package lsmkv

import (
	"errors"
	"fmt"
	"maps"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

const modelKeys = 40

type modelRun struct {
	t     *testing.T
	seed  uint64
	step  int
	ops   []string
	db    *DB
	model map[string]string
}

func (r *modelRun) failf(format string, args ...any) {
	r.t.Helper()
	last := r.ops[max(0, len(r.ops)-8):]
	r.t.Fatalf("seed=%d step=%d: %s\nlast ops:\n  %s\nreplay: go test -run 'TestModel/seed=%d$'",
		r.seed, r.step, fmt.Sprintf(format, args...), strings.Join(last, "\n  "), r.seed)
}

func (r *modelRun) logf(format string, args ...any) {
	r.ops = append(r.ops, fmt.Sprintf(format, args...))
}

// expectScan is what a correct Scan(lo, hi) returns, cut at limit entries.
func (r *modelRun) expectScan(lo, hi string, limit int) []string {
	var want []string
	for _, k := range slices.Sorted(maps.Keys(r.model)) {
		if k >= lo && (hi == "" || k < hi) && len(want) < limit {
			want = append(want, k+"="+r.model[k])
		}
	}
	return want
}

func (r *modelRun) scan(lo, hi string, limit int) []string {
	var got []string
	seq, scanErr := r.db.Scan(lo, hi)
	for k, v := range seq {
		if len(got) == limit {
			break
		}
		got = append(got, k+"="+string(v))
	}
	if err := scanErr(); err != nil {
		r.failf("scan error: %v", err)
	}
	return got
}

// check compares the DB with the model through every read path.
func (r *modelRun) check() {
	r.t.Helper()
	if got, want := r.scan("", "", 1<<30), r.expectScan("", "", 1<<30); !slices.Equal(got, want) {
		r.failf("full scan\n got  %v\n want %v", got, want)
	}
	for i := range modelKeys + 2 { // two keys that are never written
		k := fmt.Sprintf("k%02d", i)
		v, err := r.db.Get(k)
		want, ok := r.model[k]
		switch {
		case ok && (err != nil || string(v) != want):
			r.failf("Get(%s) = %q, %v; want %q", k, v, err, want)
		case !ok && !errors.Is(err, ErrNotFound):
			r.failf("Get(%s) = %q, %v; want ErrNotFound", k, v, err)
		}
	}
}

func runModel(t *testing.T, seed uint64, steps int) (flushes, compactions int) {
	rng := rand.New(rand.NewPCG(seed, seed))
	opts := Options{Sync: SyncNever, MemtableBytes: 700, BlockBytes: 96, CompactAt: 4}
	r := &modelRun{t: t, seed: seed, model: map[string]string{}}
	r.db = mustOpen(t, t.TempDir(), opts)
	dir := r.db.dir
	key := func() string { return fmt.Sprintf("k%02d", rng.IntN(modelKeys)) }

	for r.step = range steps {
		before := r.db.Stats().Tables
		switch p := rng.IntN(100); {
		case p < 40:
			k, v := key(), strings.Repeat(string(rune('a'+rng.IntN(26))), rng.IntN(25))
			r.logf("put %s %q", k, v)
			if err := r.db.Put(k, []byte(v)); err != nil {
				r.failf("put: %v", err)
			}
			r.model[k] = v
		case p < 55:
			k := key()
			r.logf("delete %s", k)
			if err := r.db.Delete(k); err != nil {
				r.failf("delete: %v", err)
			}
			delete(r.model, k)
		case p < 70:
			r.logf("get")
		case p < 82:
			lo, hi, limit := key(), key(), 1+rng.IntN(15)
			if rng.IntN(4) == 0 {
				hi = ""
			}
			r.logf("scan [%s,%s) limit %d", lo, hi, limit)
			if got, want := r.scan(lo, hi, limit), r.expectScan(lo, hi, limit); !slices.Equal(got, want) {
				r.failf("scan [%s,%s) limit %d\n got  %v\n want %v", lo, hi, limit, got, want)
			}
		case p < 90:
			r.logf("flush")
			if err := r.db.Flush(); err != nil {
				r.failf("flush: %v", err)
			}
		case p < 94:
			r.logf("compact")
			if err := r.db.Compact(); err != nil {
				r.failf("compact: %v", err)
			}
		default:
			r.logf("reopen")
			if err := r.db.Close(); err != nil {
				r.failf("close: %v", err)
			}
			var err error
			if r.db, err = Open(dir, opts); err != nil {
				r.failf("reopen: %v", err)
			}
			t.Cleanup(func(db *DB) func() { return func() { db.Close() } }(r.db))
			before = r.db.Stats().Tables
		}
		if after := r.db.Stats().Tables; after > before {
			flushes++
		} else if after < before {
			compactions++
		}
		r.check()
	}
	return flushes, compactions
}

// TestModel replays seeded random histories against a map after every step.
// A failure prints the seed and the last operations; re-run just that seed
// with the printed command.
func TestModel(t *testing.T) {
	for seed := uint64(1); seed <= 8; seed++ {
		t.Run(fmt.Sprintf("seed=%d", seed), func(t *testing.T) {
			t.Parallel()
			flushes, compactions := runModel(t, seed, 500)
			if flushes < 3 || compactions < 1 {
				t.Fatalf("seed %d exercised %d flushes and %d compactions; the test is not testing", seed, flushes, compactions)
			}
		})
	}
}
