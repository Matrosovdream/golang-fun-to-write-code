package parse_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"metricsd/internal/parse"
)

func TestParseValid(t *testing.T) {
	tests := []struct {
		line string
		want parse.Event
	}{
		{"c http_requests 1", parse.Event{Kind: parse.KindCounter, Name: []byte("http_requests"), Value: 1}},
		{"g queue_depth 42", parse.Event{Kind: parse.KindGauge, Name: []byte("queue_depth"), Value: 42}},
		{"h latency_ms 250", parse.Event{Kind: parse.KindHistogram, Name: []byte("latency_ms"), Value: 250}},
		{"g temp -15", parse.Event{Kind: parse.KindGauge, Name: []byte("temp"), Value: -15}},
	}
	for _, tc := range tests {
		got, err := parse.Parse([]byte(tc.line))
		require.NoError(t, err, tc.line)
		require.Equal(t, tc.want, got, tc.line)
	}
}

func TestParseInvalid(t *testing.T) {
	for _, line := range []string{
		"", "c", "c name", "x name 1", "c  1", "c name 1x", "c name ",
		"c name 99999999999999999999", "c name -", "cname 1",
	} {
		_, err := parse.Parse([]byte(line))
		require.Error(t, err, "line %q", line)
	}
}

// reference is the "obviously correct" implementation. The fuzz target
// compares the fast parser against it — differential fuzzing.
func reference(line string) (parse.Event, bool) {
	fields := strings.Fields(line)
	if len(fields) != 3 || len(fields[0]) != 1 || !strings.ContainsAny(fields[0], "cgh") {
		return parse.Event{}, false
	}
	// strings.Fields collapses runs of spaces; the real parser demands
	// exactly one space, so reconstruct and compare.
	if line != fields[0]+" "+fields[1]+" "+fields[2] {
		return parse.Event{}, false
	}
	for _, c := range []byte(fields[1]) { // names: printable ASCII only
		if c <= ' ' || c >= 0x7f {
			return parse.Event{}, false
		}
	}
	// ParseInt is laxer than the wire spec ("+5"), so exclude that too.
	if strings.HasPrefix(fields[2], "+") {
		return parse.Event{}, false
	}
	v, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil {
		return parse.Event{}, false
	}
	return parse.Event{Kind: parse.Kind(fields[0][0]), Name: []byte(fields[1]), Value: v}, true
}

func FuzzParse(f *testing.F) {
	f.Add("c http_requests 1")
	f.Add("g queue -42")
	f.Add("h latency 100")
	f.Add("c  1")
	f.Add("💥 x 1")

	f.Fuzz(func(t *testing.T, line string) {
		got, err := parse.Parse([]byte(line))
		want, ok := reference(line)
		if ok != (err == nil) {
			t.Fatalf("parsers disagree on %q: fast err=%v, reference ok=%v", line, err, ok)
		}
		if ok {
			require.Equal(t, want.Kind, got.Kind)
			require.Equal(t, string(want.Name), string(got.Name))
			require.Equal(t, want.Value, got.Value)
		}
	})
}

func BenchmarkParseFast(b *testing.B) {
	line := []byte("h http_request_duration_ms 237")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := parse.Parse(line); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseReference(b *testing.B) {
	line := "h http_request_duration_ms 237"
	b.ReportAllocs()
	for b.Loop() {
		if _, ok := reference(line); !ok {
			b.Fatal("parse failed")
		}
	}
}
