package lsmkv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func Open(dir string, opts Options) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	lock, err := lockDir(dir)
	if err != nil {
		return nil, fmt.Errorf("lsmkv: open %s: %w", dir, err)
	}
	d := &DB{dir: dir, opts: opts.withDefaults(), lock: lock, mem: newMemtable()}
	if err := d.recover(); err != nil {
		d.closeFiles()
		return nil, fmt.Errorf("lsmkv: open %s: %w", dir, err)
	}
	return d, nil
}

func (d *DB) recover() error {
	m, ok, err := readManifest(d.dir)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return err
	}
	if !ok {
		for _, de := range entries {
			if strings.HasSuffix(de.Name(), ".sst") {
				return corruptf("MANIFEST is missing but %s exists", de.Name())
			}
		}
		m = manifest{Wal: 1, Next: 2}
		if err := writeManifest(d.dir, m); err != nil {
			return err
		}
	}
	if err := sweep(d.dir, entries, m); err != nil {
		return err
	}
	for _, num := range m.Tables {
		t, err := openTable(sstPath(d.dir, num), num)
		if errors.Is(err, fs.ErrNotExist) {
			return corruptf("manifest lists table %06d but the file is missing", num)
		}
		if err != nil {
			return err
		}
		d.tables = append(d.tables, t)
	}
	d.walNum, d.next = m.Wal, m.Next
	return d.replayWAL()
}

// sweep deletes what a crash can leave behind: temp files, tables the
// manifest does not list (flush or compaction died before its commit, or
// after it but before deleting inputs), and logs the manifest has moved past.
func sweep(dir string, entries []fs.DirEntry, m manifest) error {
	live := make(map[uint64]bool, len(m.Tables))
	for _, n := range m.Tables {
		live[n] = true
	}
	for _, de := range entries {
		name := de.Name()
		base, ext, _ := strings.Cut(name, ".")
		num, err := strconv.ParseUint(base, 10, 64)
		numbered := err == nil
		if strings.HasSuffix(name, ".tmp") ||
			numbered && (ext == "sst" && !live[num] || ext == "wal" && num < m.Wal) {
			if err := os.Remove(filepath.Join(dir, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *DB) replayWAL() error {
	path := walPath(d.dir, d.walNum)
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	valid, err := decodeWAL(data, d.mem.put)
	if err != nil {
		return err
	}
	if valid < len(data) {
		if err := os.Truncate(path, int64(valid)); err != nil {
			return err
		}
	}
	if d.wal, err = openLog(d.dir, d.walNum); err != nil {
		return err
	}
	d.lastSync = time.Now()
	return nil
}

func (d *DB) closeFiles() error {
	var err error
	if d.wal != nil {
		err = d.wal.close()
	}
	for _, t := range d.tables {
		t.unref()
	}
	return errors.Join(err, d.lock.Close())
}
