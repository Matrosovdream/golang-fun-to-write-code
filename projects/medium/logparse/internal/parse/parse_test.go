package parse_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"logparse/internal/parse"
)

const sample = `192.168.1.10 [30/Sep/2026:12:00:00] "GET /api/users" 200 1234 12.345`

var parsers = map[string]func(string) (parse.Line, error){
	"naive": parse.ParseNaive,
	"fast":  parse.ParseFast,
}

// Both parsers must agree on everything — the fast one is only allowed to be
// faster, never different.
func TestParsersAgree(t *testing.T) {
	lines := []string{
		sample,
		`10.0.0.1 [01/Jan/2026:00:00:00] "POST /login" 401 89 3.2`,
		`2001:db8::1 [01/Jan/2026:00:00:00] "DELETE /api/items/42" 204 0 0.751`,
	}
	for _, line := range lines {
		want, err := parse.ParseNaive(line)
		require.NoError(t, err)
		got, err := parse.ParseFast(line)
		require.NoError(t, err)
		require.Equal(t, want, got, "line: %s", line)
	}
}

func TestParseFields(t *testing.T) {
	for name, p := range parsers {
		t.Run(name, func(t *testing.T) {
			l, err := p(sample)
			require.NoError(t, err)
			require.Equal(t, parse.Line{
				IP: "192.168.1.10", Method: "GET", Path: "/api/users",
				Status: 200, Bytes: 1234, LatencyMs: 12.345,
			}, l)
		})
	}
}

func TestMalformedLines(t *testing.T) {
	bad := []string{
		"",
		"not a log line",
		`1.2.3.4 [ts] "GET /x" 20x 5 1.0`,
		`1.2.3.4 [ts] "GET /x" 200 5`, // missing latency
	}
	for name, p := range parsers {
		t.Run(name, func(t *testing.T) {
			for _, line := range bad {
				_, err := p(line)
				require.ErrorIs(t, err, parse.ErrMalformed, "line: %q", line)
			}
		})
	}
}
