package hub

// ring keeps the last N messages for history replay. Fixed array + write
// index — O(1) append, no allocation after creation.
type ring struct {
	buf  []Envelope
	next int
	full bool
}

func newRing(n int) *ring {
	return &ring{buf: make([]Envelope, n)}
}

func (r *ring) add(e Envelope) {
	r.buf[r.next] = e
	r.next = (r.next + 1) % len(r.buf)
	if r.next == 0 {
		r.full = true
	}
}

// list returns messages oldest-first.
func (r *ring) list() []Envelope {
	if !r.full {
		return append([]Envelope(nil), r.buf[:r.next]...)
	}
	out := make([]Envelope, 0, len(r.buf))
	out = append(out, r.buf[r.next:]...)
	return append(out, r.buf[:r.next]...)
}
