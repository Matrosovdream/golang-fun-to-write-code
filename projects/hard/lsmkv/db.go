// Package lsmkv is an embedded LSM-tree key-value store: durable, ordered,
// single process. Keys are strings, values are byte slices.
//
// # Design (every on-disk format lives in this comment; retype from it)
//
// Write path: Put/Delete append a WAL record, fsync it according to the
// SyncPolicy, then insert into the memtable (a skiplist). When the memtable
// passes MemtableBytes it is flushed to an immutable SSTable and the WAL it
// was protecting is deleted. Appending is sequential I/O, which is why this
// design writes fast.
//
// Read path: the memtable, then SSTables newest to oldest; the first source
// holding the key wins, and a tombstone counts as a hit that says "deleted".
// Each table has a bloom filter that skips it when the key is surely absent.
// Scan merges the memtable and every table with a heap; on equal keys the
// newest source wins.
//
// Directory:
//
//	LOCK          flock'd while the DB is open
//	MANIFEST      the live set (text, below); replaced by atomic rename
//	NNNNNN.wal    the log named by the manifest; older logs are garbage
//	NNNNNN.sst    tables; only those listed in the manifest are live
//	*.tmp         half-written files, never live
//
// Entry (WAL payload and SSTable block body):
//
//	kind (1 byte: 0 put, 1 delete) | uvarint klen | uvarint vlen | key | value
//	a delete is an entry with vlen 0: tombstones are records, not absences.
//
// WAL record:
//
//	uvarint len(payload) | crc32c(payload) (4 bytes LE) | payload
//
// Replay stops at the first bad record. A record cut short, or one whose
// checksum fails and which ends exactly at EOF, is a torn tail: dropped, and
// the file truncated there. A bad checksum with bytes after it is ErrCorrupt.
//
// SSTable:
//
//	data block 0 .. data block n-1 | index block | bloom block | footer
//
//	every block   = body | crc32c(body) (4 bytes LE)
//	data body     = entries in ascending key order, about BlockBytes long
//	index body    = entries; key = LAST key of a data block,
//	                value = uvarint offset | uvarint size (size includes the crc)
//	bloom body    = bit array | k (1 byte); positions come from hashKey (FNV-1a)
//	footer (40 B) = indexOff, indexSize, bloomOff, bloomSize, magic "lsmkv001"
//	                as five little-endian uint64
//
// Manifest (JSON; tables are oldest first, so later means newer):
//
//	{"wal":7,"next":12,"tables":[3,5,9]}
//
// Flush order, each step surviving a crash before the next:
//
//  1. write NNNNNN.sst.tmp, fsync, rename to NNNNNN.sst, fsync the directory
//     (crash: an orphan or tmp file, the old WAL still has every write)
//  2. write MANIFEST.tmp naming the new table and a NEW wal number, fsync,
//     rename over MANIFEST, fsync the directory   <- the commit point
//  3. create the new WAL, fsync the directory
//  4. delete the old WAL (crash: it is older than the manifest's wal, swept)
//
// Compaction merges all tables into one with the same steps 1, 2 and then
// deletes the inputs. Dropping tombstones is only safe because the merge
// covers every table: nothing older is left for a tombstone to hide.
//
// Open undoes whatever a crash left: *.tmp files, tables the manifest does not
// list and logs older than the manifest's wal are deleted, then the current
// WAL is replayed into a fresh memtable and truncated to its intact prefix.
//
// Concurrency: one RWMutex. Writes, flushes and compactions hold it
// exclusively (so a flush stalls readers; a background flush is an exercise).
// Readers hold it only long enough to pin the memtable and take a reference
// on each table; they then read lock-free, and a compaction can delete files
// under an unfinished Scan because the last reference closes and removes them.
package lsmkv

import (
	"cmp"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("lsmkv: key not found")
	ErrClosed   = errors.New("lsmkv: database closed")
	ErrLocked   = errors.New("lsmkv: directory is open in another DB (this process or another)")
	ErrCorrupt  = errors.New("lsmkv: corrupt data")
)

// SyncPolicy says when the WAL is fsynced, i.e. which acknowledged writes
// survive a power loss (a process crash loses nothing: writes reach the OS).
type SyncPolicy int

const (
	SyncEvery    SyncPolicy = iota // fsync before every write returns: nothing acknowledged is lost
	SyncPeriodic                   // fsync on a write when SyncPeriod has passed since the last: loses up to SyncPeriod
	SyncNever                      // never fsync the WAL, not even on Close; Flush still fsyncs its table
)

// Options's zero value is usable: zero fields take the defaults noted.
type Options struct {
	Sync          SyncPolicy
	SyncPeriod    time.Duration // default 100ms
	MemtableBytes int           // flush threshold, default 4 MiB
	BlockBytes    int           // target data block size, default 4 KiB
	BloomBits     int           // bloom bits per key, default 10; negative disables the filter
	CompactAt     int           // compact when this many tables exist, default 4
}

func (o Options) withDefaults() Options {
	o.SyncPeriod = cmp.Or(o.SyncPeriod, 100*time.Millisecond)
	o.MemtableBytes = cmp.Or(o.MemtableBytes, 4<<20)
	o.BlockBytes = cmp.Or(o.BlockBytes, 4<<10)
	o.BloomBits = cmp.Or(o.BloomBits, 10)
	o.CompactAt = cmp.Or(o.CompactAt, 4)
	return o
}

type DB struct {
	dir  string
	opts Options
	lock *os.File

	mu       sync.RWMutex
	mem      *memtable
	wal      *walWriter
	walNum   uint64
	tables   []*table // oldest first; replaced on change, never modified in place
	next     uint64   // next unused file number
	lastSync time.Time
	failed   error // sticky: set when the on-disk state is no longer trustworthy
	closed   bool
}

func walPath(dir string, num uint64) string { return filepath.Join(dir, fmt.Sprintf("%06d.wal", num)) }
func sstPath(dir string, num uint64) string { return filepath.Join(dir, fmt.Sprintf("%06d.sst", num)) }

func (d *DB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return ErrClosed
	}
	d.closed = true
	var err error
	if d.opts.Sync != SyncNever {
		err = d.wal.sync()
	}
	return errors.Join(err, d.closeFiles())
}

func (d *DB) writable() error {
	if d.closed {
		return ErrClosed
	}
	return d.failed
}

func (d *DB) fail(err error) error {
	d.failed = fmt.Errorf("lsmkv: database failed, reopen it to recover: %w", err)
	return d.failed
}

// Put stores a copy of value; a nil or empty value is a real, empty value.
func (d *DB) Put(key string, value []byte) error {
	return d.write(entry{key: key, val: slices.Clone(value)})
}

func (d *DB) Delete(key string) error { return d.write(entry{key: key, tomb: true}) }

func (d *DB) write(e entry) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.writable(); err != nil {
		return err
	}
	// A failed or partial WAL write may leave a torn record that later
	// appends would follow, so any WAL error poisons the DB.
	if err := d.wal.append(e); err != nil {
		return d.fail(err)
	}
	if err := d.syncPerPolicy(); err != nil {
		return d.fail(err)
	}
	d.mem.put(e)
	if d.mem.bytes.Load() < int64(d.opts.MemtableBytes) {
		return nil
	}
	return d.flushLocked()
}

func (d *DB) syncPerPolicy() error {
	switch d.opts.Sync {
	case SyncEvery:
		return d.wal.sync()
	case SyncPeriodic:
		if now := time.Now(); now.Sub(d.lastSync) >= d.opts.SyncPeriod {
			d.lastSync = now
			return d.wal.sync()
		}
	}
	return nil
}

// view is what a reader pins: the memtable and the table list as of one moment.
type view struct {
	mem    *memtable
	tables []*table
}

func (d *DB) pin() (view, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if d.closed {
		return view{}, ErrClosed
	}
	for _, t := range d.tables {
		t.ref()
	}
	return view{d.mem, d.tables}, nil
}

func (v view) unpin() {
	for _, t := range v.tables {
		t.unref()
	}
}

// sources lists everything to merge for a scan, oldest first: tables in
// manifest order, then the memtable.
func (v view) sources(lo string) []source {
	srcs := make([]source, 0, len(v.tables)+1)
	for _, t := range v.tables {
		srcs = append(srcs, t.iter(lo))
	}
	return append(srcs, v.mem.cursor(lo))
}

// Get returns a copy of the value, or ErrNotFound.
func (d *DB) Get(key string) ([]byte, error) {
	v, err := d.pin()
	if err != nil {
		return nil, err
	}
	defer v.unpin()
	e, ok := v.mem.get(key)
	hash := hashKey(key)
	for i := len(v.tables) - 1; !ok && i >= 0; i-- {
		if e, ok, err = v.tables[i].get(key, hash); err != nil {
			return nil, err
		}
	}
	if !ok || e.tomb {
		return nil, ErrNotFound
	}
	return slices.Clone(e.val), nil
}

// Scan iterates the live keys in [lo, hi) in ascending order; an empty hi
// means no upper bound. The values are copies.
//
// An iter.Seq2 cannot carry an error, so Scan also returns an accessor, in
// the style of sql.Rows.Err: call it after the loop. A nil error means the
// loop saw every key in range (or the caller broke out). A scan is not a
// snapshot: it sees the memtable as it was pinned, plus writes made to that
// memtable while the scan runs. Calling the DB from inside the loop is safe.
func (d *DB) Scan(lo, hi string) (iter.Seq2[string, []byte], func() error) {
	var scanErr error
	seq := func(yield func(string, []byte) bool) {
		scanErr = nil
		v, err := d.pin()
		if err != nil {
			scanErr = err
			return
		}
		defer v.unpin()
		m := newMerger(v.sources(lo))
		for {
			e, ok := m.next()
			if !ok || hi != "" && e.key >= hi {
				break
			}
			if e.tomb {
				continue
			}
			if !yield(e.key, slices.Clone(e.val)) {
				return
			}
		}
		scanErr = m.err()
	}
	return seq, func() error { return scanErr }
}

type Stats struct {
	Tables        int
	TableBytes    int64
	MemtableBytes int64
}

func (d *DB) Stats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	s := Stats{Tables: len(d.tables), MemtableBytes: d.mem.bytes.Load()}
	for _, t := range d.tables {
		s.TableBytes += t.size
	}
	return s
}
