package lsmkv

import "math"

// bloom is a Bloom filter stored as a byte slice: the bit array, then one
// trailing byte holding k, the number of probes. nil means "no filter", and
// every method treats it (and any unreadable k) as "may contain".
type bloom []byte

// hashKey is FNV-1a (64-bit) with a murmur finalizer. It is written to disk
// indirectly (the filter bits depend on it), so it must be stable across
// processes and releases: hash/maphash is deliberately NOT used because its
// seed is random per process.
func hashKey(key string) uint64 {
	h := uint64(14695981039346656037)
	for i := range len(key) {
		h ^= uint64(key[i])
		h *= 1099511628211
	}
	h ^= h >> 33
	h *= 0xff51afd7ed558ccd
	h ^= h >> 33
	return h
}

// split derives every probe position from one 64-bit hash (Kirsch-Mitzenmacher
// double hashing): position_i = h1 + i*h2, so k hash functions cost one.
func split(h uint64) (h1, h2 uint64) { return h & math.MaxUint32, h>>32 | 1 }

// buildBloom sizes the filter at bitsPerKey bits per key; 10 bits and
// k = 0.69*10 = 7 probes give about a 1% false-positive rate.
func buildBloom(hashes []uint64, bitsPerKey int) bloom {
	if bitsPerKey <= 0 || len(hashes) == 0 {
		return nil
	}
	k := max(1, min(30, int(float64(bitsPerKey)*math.Ln2)))
	b := make(bloom, (max(64, len(hashes)*bitsPerKey)+7)/8+1)
	b[len(b)-1] = byte(k)
	nbits := uint64(len(b)-1) * 8
	for _, h := range hashes {
		h1, h2 := split(h)
		for i := range uint64(k) {
			pos := (h1 + i*h2) % nbits
			b[pos/8] |= 1 << (pos % 8)
		}
	}
	return b
}

func (b bloom) mayContain(h uint64) bool {
	if len(b) < 2 {
		return true
	}
	k := int(b[len(b)-1])
	if k < 1 || k > 30 {
		return true
	}
	nbits := uint64(len(b)-1) * 8
	h1, h2 := split(h)
	for i := range uint64(k) {
		pos := (h1 + i*h2) % nbits
		if b[pos/8]&(1<<(pos%8)) == 0 {
			return false
		}
	}
	return true
}
