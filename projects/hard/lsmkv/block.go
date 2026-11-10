package lsmkv

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
)

// entry is one record: a value, or a tombstone (tomb) that hides older
// versions of the key in older sources. Deletes are entries, not absences.
type entry struct {
	key  string
	val  []byte
	tomb bool
}

const (
	kindPut    byte = 0
	kindDelete byte = 1
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

func corruptf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}

// appendEntry encodes kind | uvarint(klen) | uvarint(vlen) | key | val.
// WAL records and SSTable blocks share this encoding.
func appendEntry(dst []byte, e entry) []byte {
	kind := kindPut
	if e.tomb {
		kind, e.val = kindDelete, nil
	}
	dst = append(dst, kind)
	dst = binary.AppendUvarint(dst, uint64(len(e.key)))
	dst = binary.AppendUvarint(dst, uint64(len(e.val)))
	dst = append(dst, e.key...)
	return append(dst, e.val...)
}

// decodeEntry never panics on arbitrary bytes. The returned value aliases b,
// capped so that an append by the caller cannot overwrite the next entry.
func decodeEntry(b []byte) (e entry, n int, err error) {
	if len(b) == 0 {
		return e, 0, corruptf("empty entry")
	}
	kind, n := b[0], 1
	klen, w := binary.Uvarint(b[n:])
	if w <= 0 {
		return e, 0, corruptf("bad key length")
	}
	n += w
	vlen, w := binary.Uvarint(b[n:])
	if w <= 0 {
		return e, 0, corruptf("bad value length")
	}
	n += w
	if rest := uint64(len(b) - n); klen > rest || vlen > rest-klen {
		return e, 0, corruptf("entry overruns its block")
	}
	e.key = string(b[n : n+int(klen)])
	n += int(klen)
	e.val = b[n : n+int(vlen) : n+int(vlen)]
	n += int(vlen)
	switch {
	case kind == kindPut:
	case kind == kindDelete && vlen == 0:
		e.tomb = true
	default:
		return entry{}, 0, corruptf("bad entry kind %d", kind)
	}
	return e, n, nil
}

func decodeBlock(b []byte) ([]entry, error) {
	var es []entry
	for len(b) > 0 {
		e, n, err := decodeEntry(b)
		if err != nil {
			return nil, err
		}
		es = append(es, e)
		b = b[n:]
	}
	return es, nil
}

// appendCRC and stripCRC frame a block: payload | crc32c(payload) (4 bytes LE).
func appendCRC(b []byte) []byte {
	return binary.LittleEndian.AppendUint32(b, crc32.Checksum(b, crcTable))
}

func stripCRC(b []byte) ([]byte, error) {
	if len(b) < 4 {
		return nil, corruptf("block shorter than its checksum")
	}
	payload, sum := b[:len(b)-4], binary.LittleEndian.Uint32(b[len(b)-4:])
	if crc32.Checksum(payload, crcTable) != sum {
		return nil, corruptf("block checksum mismatch")
	}
	return payload, nil
}
