package lsmkv

import (
	"encoding/binary"
	"hash/crc32"
	"io"
	"os"
)

// walFile is the slice of *os.File the log needs; tests wrap it to count
// Sync calls.
type walFile interface {
	io.Writer
	Sync() error
	Close() error
}

type walWriter struct{ f walFile }

// openLog opens (creating if needed) the log for appending and fsyncs the
// directory, so the file's existence survives a crash too.
func openLog(dir string, num uint64) (*walWriter, error) {
	f, err := os.OpenFile(walPath(dir, num), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syncDir(dir); err != nil {
		f.Close()
		return nil, err
	}
	return &walWriter{f}, nil
}

// append writes one record: uvarint(len(payload)) | crc32c(payload) | payload,
// with a single Write so a record is never interleaved with another.
func (w *walWriter) append(e entry) error {
	payload := appendEntry(nil, e)
	rec := binary.AppendUvarint(make([]byte, 0, len(payload)+binary.MaxVarintLen64+4), uint64(len(payload)))
	rec = binary.LittleEndian.AppendUint32(rec, crc32.Checksum(payload, crcTable))
	_, err := w.f.Write(append(rec, payload...))
	return err
}

func (w *walWriter) sync() error  { return w.f.Sync() }
func (w *walWriter) close() error { return w.f.Close() }

// decodeWAL feeds every intact record to fn and returns the length of the
// intact prefix; the caller truncates the file there.
//
// A record that is cut short, or whose checksum fails and which ends exactly
// at EOF, is a torn tail from a crash mid-append: dropped silently. A bad
// checksum with more bytes after the record cannot be explained by a crash
// and is reported as ErrCorrupt. (A damaged length field that points past
// EOF is indistinguishable from a torn write; the result is still a prefix.)
func decodeWAL(data []byte, fn func(entry)) (int, error) {
	pos := 0
	for pos < len(data) {
		n, w := binary.Uvarint(data[pos:])
		switch {
		case w == 0:
			return pos, nil
		case w < 0:
			return pos, corruptf("wal offset %d: bad length", pos)
		}
		rest := len(data) - pos - w
		if rest < 4 || n > uint64(rest-4) {
			return pos, nil
		}
		sum := binary.LittleEndian.Uint32(data[pos+w:])
		end := pos + w + 4 + int(n)
		payload := data[pos+w+4 : end]
		if crc32.Checksum(payload, crcTable) != sum {
			if end == len(data) {
				return pos, nil
			}
			return pos, corruptf("wal offset %d: checksum mismatch", pos)
		}
		e, m, err := decodeEntry(payload)
		if err != nil || m != len(payload) {
			return pos, corruptf("wal offset %d: undecodable record", pos)
		}
		fn(e)
		pos = end
	}
	return pos, nil
}
