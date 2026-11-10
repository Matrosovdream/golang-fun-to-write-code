package lsmkv

import (
	"errors"
	"os"
	"slices"
	"time"
)

// Flush writes the memtable to a new SSTable.
func (d *DB) Flush() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.writable(); err != nil {
		return err
	}
	return d.flushLocked()
}

func (d *DB) flushLocked() error {
	if d.mem.sl.empty() {
		return nil
	}
	tableNum, walNum := d.next, d.next+1
	d.next += 2
	// Tombstones must survive a flush: older tables may hold the key.
	if _, err := d.writeTable(tableNum, d.mem.cursor(""), true); err != nil {
		return err
	}
	t, err := openTable(sstPath(d.dir, tableNum), tableNum)
	if err != nil {
		return err
	}
	tables := slices.Concat(d.tables, []*table{t})
	if err := writeManifest(d.dir, manifest{Wal: walNum, Next: d.next, Tables: tableNums(tables)}); err != nil {
		t.unref()
		return d.fail(err) // the rename may or may not have happened: only Open can tell
	}
	wal, err := openLog(d.dir, walNum)
	if err != nil {
		t.unref()
		return d.fail(err)
	}
	oldWAL, oldNum := d.wal, d.walNum
	d.wal, d.walNum, d.tables, d.mem, d.lastSync = wal, walNum, tables, newMemtable(), time.Now()
	oldWAL.close()
	os.Remove(walPath(d.dir, oldNum)) // if this fails, Open sweeps it
	if len(d.tables) >= d.opts.CompactAt {
		return d.compactLocked()
	}
	return nil
}

// Compact merges every table into one, dropping tombstones and overwritten
// values. It runs automatically after a flush once CompactAt tables exist.
func (d *DB) Compact() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.writable(); err != nil {
		return err
	}
	return d.compactLocked()
}

func (d *DB) compactLocked() error {
	if len(d.tables) < 2 {
		return nil
	}
	num := d.next
	d.next++
	srcs := make([]source, len(d.tables))
	for i, t := range d.tables {
		srcs[i] = t.iter("")
	}
	n, err := d.writeTable(num, newMerger(srcs), false)
	if err != nil {
		return err
	}
	var merged []*table // empty when every key had been deleted
	if n > 0 {
		t, err := openTable(sstPath(d.dir, num), num)
		if err != nil {
			return err
		}
		merged = []*table{t}
	}
	if err := writeManifest(d.dir, manifest{Wal: d.walNum, Next: d.next, Tables: tableNums(merged)}); err != nil {
		for _, t := range merged {
			t.unref()
		}
		return d.fail(err)
	}
	old := d.tables
	d.tables = merged
	for _, t := range old {
		t.obsolete.Store(true) // deleted when the last reader lets go
		t.unref()
	}
	return nil
}

// writeTable streams src into NNNNNN.sst crash-safely (temp file, fsync,
// rename, fsync the directory) and returns the entry count. With nothing to
// write it creates no file.
func (d *DB) writeTable(num uint64, src source, keepTombstones bool) (int, error) {
	final := sstPath(d.dir, num)
	tmp := final + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	tw := newTableWriter(f, d.opts.BlockBytes, d.opts.BloomBits)
	err = drain(tw, src, keepTombstones)
	if err == nil && tw.count > 0 {
		err = errors.Join(tw.finish(), f.Sync())
	}
	if err = errors.Join(err, f.Close()); err != nil || tw.count == 0 {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, final); err != nil {
		return 0, err
	}
	return tw.count, syncDir(d.dir)
}

func drain(tw *tableWriter, src source, keepTombstones bool) error {
	for {
		e, ok := src.next()
		if !ok {
			return src.err()
		}
		if e.tomb && !keepTombstones {
			continue
		}
		if err := tw.add(e); err != nil {
			return err
		}
	}
}

func tableNums(ts []*table) []uint64 {
	nums := make([]uint64, len(ts))
	for i, t := range ts {
		nums[i] = t.num
	}
	return nums
}
