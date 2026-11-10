package lb

import (
	"cmp"
	"hash/crc32"
	"slices"
	"sort"
	"strconv"
)

type point struct {
	hash  uint32
	owner *Backend
}

// ring is a consistent-hash ring. Each backend owns replicas*weight virtual
// points; a key belongs to the first point clockwise from its hash. Removing a
// backend only reassigns the keys that sat on its points, and every one of
// them moves to the next point clockwise, never between surviving backends.
type ring struct{ points []point }

func newRing(backends []*Backend, replicas int) *ring {
	var r ring
	for _, b := range backends {
		for i := range replicas * b.Weight {
			h := crc32.ChecksumIEEE([]byte(b.URL.Host + "#" + strconv.Itoa(i)))
			r.points = append(r.points, point{h, b})
		}
	}
	slices.SortFunc(r.points, func(a, b point) int { return cmp.Compare(a.hash, b.hash) })
	return &r
}

// Pick walks clockwise from the key's hash to the first point whose owner is a
// candidate. Skipping unavailable owners this way gives the same answer as
// rebuilding the ring without them, so health changes remap minimally too.
func (r *ring) Pick(key string, c []*Backend) *Backend {
	h := crc32.ChecksumIEEE([]byte(key))
	start := sort.Search(len(r.points), func(i int) bool { return r.points[i].hash >= h })
	for i := range r.points {
		if b := r.points[(start+i)%len(r.points)].owner; slices.Contains(c, b) {
			return b
		}
	}
	return nil
}
