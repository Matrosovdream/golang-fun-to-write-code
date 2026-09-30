// Package parse decodes the wire protocol, one line per metric event:
//
//	c <name> <value>\n    counter += value
//	g <name> <value>\n    gauge   = value
//	h <name> <value>\n    histogram observe value
//
// The parser is zero-allocation: it returns the name as a subslice of the
// input and converts the value with a hand-rolled atoi. At millions of
// lines/second, one allocation per line IS the bottleneck (see the
// benchmark against the strings.Fields version).
package parse

import "errors"

type Kind byte

const (
	KindCounter   Kind = 'c'
	KindGauge     Kind = 'g'
	KindHistogram Kind = 'h'
)

var (
	ErrMalformed = errors.New("malformed metric line")
	ErrBadValue  = errors.New("bad metric value")
)

type Event struct {
	Kind  Kind
	Name  []byte // subslice of the input — copy it if you keep it!
	Value int64
}

func Parse(line []byte) (Event, error) {
	// "<k> <name> <value>" — minimum is "c a 0" = 5 bytes.
	if len(line) < 5 || line[1] != ' ' {
		return Event{}, ErrMalformed
	}
	kind := Kind(line[0])
	if kind != KindCounter && kind != KindGauge && kind != KindHistogram {
		return Event{}, ErrMalformed
	}

	rest := line[2:]
	sp := indexByte(rest, ' ')
	if sp <= 0 || sp == len(rest)-1 {
		return Event{}, ErrMalformed
	}
	name := rest[:sp]
	// Names are printable ASCII only. A server parsing network input must
	// validate, not assume — the fuzzer found "\r" sneaking in here.
	for _, c := range name {
		if c <= ' ' || c >= 0x7f {
			return Event{}, ErrMalformed
		}
	}

	value, err := atoi(rest[sp+1:])
	if err != nil {
		return Event{}, err
	}
	return Event{Kind: kind, Name: name, Value: value}, nil
}

// atoi parses a signed decimal int64 without allocating. strconv.ParseInt
// is also allocation-free on success — but it costs a bounds-checked
// generality we don't need; measure the difference in the benchmark.
func atoi(b []byte) (int64, error) {
	if len(b) == 0 {
		return 0, ErrBadValue
	}
	neg := false
	if b[0] == '-' {
		neg = true
		b = b[1:]
		if len(b) == 0 {
			return 0, ErrBadValue
		}
	}
	var n int64
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, ErrBadValue
		}
		d := int64(c - '0')
		if n > (1<<63-1-d)/10 { // overflow guard
			return 0, ErrBadValue
		}
		n = n*10 + d
	}
	if neg {
		n = -n
	}
	return n, nil
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}
