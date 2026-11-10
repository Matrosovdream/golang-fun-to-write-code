package lsmkv

import (
	"bufio"
	"cmp"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"slices"
	"sync/atomic"
)

const (
	footerSize = 40
	tableMagic = 0x6c736d6b76303031 // "lsmkv001"
)

// handle locates a framed block (payload + crc) inside the file.
type handle struct{ off, size uint64 }

func (h handle) append(dst []byte) []byte {
	return binary.AppendUvarint(binary.AppendUvarint(dst, h.off), h.size)
}

func decodeHandle(b []byte) (handle, error) {
	off, n := binary.Uvarint(b)
	if n <= 0 {
		return handle{}, corruptf("bad block handle")
	}
	size, m := binary.Uvarint(b[n:])
	if m <= 0 {
		return handle{}, corruptf("bad block handle")
	}
	return handle{off, size}, nil
}

// tableWriter streams sorted entries into an SSTable. Memory use is one
// block plus 8 bytes per key (the hashes the bloom filter is built from at the end).
type tableWriter struct {
	w          *bufio.Writer
	off        uint64
	blockBytes int
	bloomBits  int
	block      []byte
	last       string
	index      []byte // index block under construction: last key of each block -> handle
	hashes     []uint64
	count      int
}

func newTableWriter(w io.Writer, blockBytes, bloomBits int) *tableWriter {
	return &tableWriter{w: bufio.NewWriter(w), blockBytes: blockBytes, bloomBits: bloomBits}
}

// add requires strictly ascending keys.
func (t *tableWriter) add(e entry) error {
	if t.count > 0 && e.key <= t.last {
		return fmt.Errorf("lsmkv: table keys out of order: %q after %q", e.key, t.last)
	}
	t.block = appendEntry(t.block, e)
	t.last = e.key
	t.hashes = append(t.hashes, hashKey(e.key))
	t.count++
	if len(t.block) >= t.blockBytes {
		return t.cutBlock()
	}
	return nil
}

func (t *tableWriter) cutBlock() error {
	if len(t.block) == 0 {
		return nil
	}
	h, err := t.writeBlock(t.block)
	t.index = appendEntry(t.index, entry{key: t.last, val: h.append(nil)})
	t.block = t.block[:0]
	return err
}

func (t *tableWriter) writeBlock(payload []byte) (handle, error) {
	framed := appendCRC(payload)
	h := handle{t.off, uint64(len(framed))}
	t.off += h.size
	_, err := t.w.Write(framed)
	return h, err
}

// finish writes the last data block, the index, the bloom filter and the footer.
func (t *tableWriter) finish() error {
	if err := t.cutBlock(); err != nil {
		return err
	}
	idx, err := t.writeBlock(t.index)
	if err != nil {
		return err
	}
	blm, err := t.writeBlock(buildBloom(t.hashes, t.bloomBits))
	if err != nil {
		return err
	}
	var foot [footerSize]byte
	for i, v := range []uint64{idx.off, idx.size, blm.off, blm.size, tableMagic} {
		binary.LittleEndian.PutUint64(foot[i*8:], v)
	}
	if _, err := t.w.Write(foot[:]); err != nil {
		return err
	}
	return t.w.Flush()
}

type indexEntry struct {
	last string // largest key in the block
	h    handle
}

// table is an open, immutable SSTable. The DB and every in-flight reader hold
// a reference; the file is closed (and deleted, if obsolete) when the last one lets go.
type table struct {
	num      uint64
	path     string
	f        *os.File
	size     int64
	index    []indexEntry
	bloom    bloom
	refs     atomic.Int32
	obsolete atomic.Bool
}

func openTable(path string, num uint64) (*table, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	t, err := loadTable(f, path, num)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("lsmkv: open table %s: %w", path, err)
	}
	t.refs.Store(1)
	return t, nil
}

func loadTable(f *os.File, path string, num uint64) (*table, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	t := &table{num: num, path: path, f: f, size: st.Size()}
	if t.size < footerSize {
		return nil, corruptf("file shorter than footer")
	}
	var foot [footerSize]byte
	if _, err := f.ReadAt(foot[:], t.size-footerSize); err != nil {
		return nil, err
	}
	var v [5]uint64
	for i := range v {
		v[i] = binary.LittleEndian.Uint64(foot[i*8:])
	}
	if v[4] != tableMagic {
		return nil, corruptf("bad magic %#x", v[4])
	}
	idx, err := t.readBlock(handle{v[0], v[1]})
	if err != nil {
		return nil, err
	}
	es, err := decodeBlock(idx)
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		h, err := decodeHandle(e.val)
		if err != nil {
			return nil, err
		}
		t.index = append(t.index, indexEntry{e.key, h})
	}
	if t.bloom, err = t.readBlock(handle{v[2], v[3]}); err != nil {
		return nil, err
	}
	return t, nil
}

// readBlock reads and checksums one framed block. The payload is a fresh
// buffer, never shared with another read.
func (t *table) readBlock(h handle) ([]byte, error) {
	if h.size < 4 || h.off > uint64(t.size) || h.size > uint64(t.size)-h.off {
		return nil, corruptf("block handle %d+%d outside file of %d bytes", h.off, h.size, t.size)
	}
	buf := make([]byte, h.size)
	if _, err := t.f.ReadAt(buf, int64(h.off)); err != nil {
		return nil, err
	}
	return stripCRC(buf)
}

func (t *table) loadEntries(i int) ([]entry, error) {
	b, err := t.readBlock(t.index[i].h)
	if err != nil {
		return nil, fmt.Errorf("table %06d block %d: %w", t.num, i, err)
	}
	return decodeBlock(b)
}

// firstBlock is the index of the first block that can hold a key >= k.
func (t *table) firstBlock(k string) int {
	i, _ := slices.BinarySearchFunc(t.index, k, func(ie indexEntry, target string) int {
		return cmp.Compare(ie.last, target)
	})
	return i
}

func (t *table) get(key string, hash uint64) (entry, bool, error) {
	if !t.bloom.mayContain(hash) {
		return entry{}, false, nil
	}
	i := t.firstBlock(key)
	if i == len(t.index) {
		return entry{}, false, nil
	}
	es, err := t.loadEntries(i)
	if err != nil {
		return entry{}, false, err
	}
	j, ok := slices.BinarySearchFunc(es, key, func(e entry, k string) int { return cmp.Compare(e.key, k) })
	if !ok {
		return entry{}, false, nil
	}
	return es[j], true, nil
}

func (t *table) ref() { t.refs.Add(1) }

func (t *table) unref() {
	if t.refs.Add(-1) > 0 {
		return
	}
	t.f.Close()
	if t.obsolete.Load() {
		os.Remove(t.path) // if this fails, Open sweeps the orphan
	}
}

// tableIter is a source over the entries >= lo, loading one block at a time.
type tableIter struct {
	t    *table
	blk  int
	lo   string // applied to the first block only
	es   []entry
	fail error
}

func (t *table) iter(lo string) *tableIter { return &tableIter{t: t, blk: t.firstBlock(lo), lo: lo} }

func (it *tableIter) next() (entry, bool) {
	for len(it.es) == 0 {
		if it.fail != nil || it.blk >= len(it.t.index) {
			return entry{}, false
		}
		es, err := it.t.loadEntries(it.blk)
		if err != nil {
			it.fail = err
			return entry{}, false
		}
		it.blk++
		j, _ := slices.BinarySearchFunc(es, it.lo, func(e entry, k string) int { return cmp.Compare(e.key, k) })
		it.es, it.lo = es[j:], ""
	}
	e := it.es[0]
	it.es = it.es[1:]
	return e, true
}

func (it *tableIter) err() error { return it.fail }
