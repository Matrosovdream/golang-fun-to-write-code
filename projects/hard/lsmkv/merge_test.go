package lsmkv

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func drainAll(s source) []entry {
	var out []entry
	for e, ok := s.next(); ok; e, ok = s.next() {
		out = append(out, e)
	}
	return out
}

func TestMergerNewestWins(t *testing.T) {
	oldest := &sliceSource{es: []entry{put("a", "old"), put("b", "old"), put("d", "old")}}
	middle := &sliceSource{es: []entry{del("b"), put("c", "mid")}}
	newest := &sliceSource{es: []entry{put("a", "new"), put("e", "new")}}

	got := drainAll(newMerger([]source{oldest, middle, newest}))
	var flat []string
	for _, e := range got {
		if e.tomb {
			flat = append(flat, e.key+"=<deleted>")
		} else {
			flat = append(flat, e.key+"="+string(e.val))
		}
	}
	// b stays a tombstone: the merger passes them on, the caller decides.
	require.Equal(t, []string{"a=new", "b=<deleted>", "c=mid", "d=old", "e=new"}, flat)
}

func TestMergerEdgeCases(t *testing.T) {
	require.Empty(t, drainAll(newMerger(nil)))
	require.Empty(t, drainAll(newMerger([]source{&sliceSource{}, &sliceSource{}})))
	same := drainAll(newMerger([]source{
		&sliceSource{es: []entry{put("k", "1")}}, &sliceSource{es: []entry{put("k", "2")}}, &sliceSource{es: []entry{put("k", "3")}},
	}))
	require.Len(t, same, 1)
	require.Equal(t, "3", string(same[0].val))
}

// If a source fails, an older source's stale version of the key must not
// surface as if it were current: the merger stops and reports the error.
func TestMergerStopsOnSourceError(t *testing.T) {
	boom := errors.New("boom")
	old := &sliceSource{es: []entry{put("a", "stale"), put("b", "stale")}}
	failing := &sliceSource{es: []entry{put("a", "fresh")}, fail: boom}
	m := newMerger([]source{old, failing})

	got := drainAll(m)
	require.Empty(t, got, "no entry may be emitted once the newest source is known to be broken")
	require.ErrorIs(t, m.err(), boom)

	m = newMerger([]source{&sliceSource{fail: boom}, &sliceSource{es: []entry{put("x", "1")}}})
	require.Empty(t, drainAll(m))
	require.ErrorIs(t, m.err(), boom)
}
