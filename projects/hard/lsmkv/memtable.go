package lsmkv

import "sync/atomic"

// memtable is the in-memory write buffer: a skiplist of the newest entry per
// key. One writer (the DB holds its write lock), any number of readers.
type memtable struct {
	sl    *skiplist[string, entry]
	bytes atomic.Int64 // approximate size; overwrites are counted again
}

const entryOverhead = 48 // rough per-entry cost of a node, so tiny entries still trigger a flush

func newMemtable() *memtable {
	return &memtable{sl: newSkiplist[string, entry](1)}
}

// put takes ownership of e.val: the slice is shared with concurrent readers
// from now on and must not be modified.
func (m *memtable) put(e entry) {
	m.sl.set(e.key, e)
	m.bytes.Add(int64(len(e.key) + len(e.val) + entryOverhead))
}

func (m *memtable) get(key string) (entry, bool) { return m.sl.get(key) }

// memCursor adapts a skiplist cursor to the source interface used by merges.
type memCursor struct{ c *slCursor[string, entry] }

func (m *memtable) cursor(from string) memCursor { return memCursor{m.sl.cursor(from)} }

func (c memCursor) next() (entry, bool) {
	_, e, ok := c.c.next()
	return e, ok
}

func (memCursor) err() error { return nil }
