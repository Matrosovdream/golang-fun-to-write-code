package analyze_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"logparse/internal/analyze"
	"logparse/internal/parse"
)

func sampleLog(lines int) string {
	var sb strings.Builder
	for i := range lines {
		ip := fmt.Sprintf("10.0.0.%d", i%5)
		status := []int{200, 200, 200, 404, 500}[i%5]
		fmt.Fprintf(&sb, "%s [30/Sep/2026:12:00:00] \"GET /page%d\" %d 100 %d.5\n",
			ip, i%3, status, i%20)
	}
	sb.WriteString("this line is garbage\n")
	return sb.String()
}

// Every strategy must produce identical stats — same discipline as the
// parsers: alternatives may only differ in speed.
func TestAllStrategiesAgree(t *testing.T) {
	data := sampleLog(1000)
	path := filepath.Join(t.TempDir(), "test.log")
	require.NoError(t, os.WriteFile(path, []byte(data), 0o644))

	seq, err := analyze.Sequential(strings.NewReader(data), parse.ParseFast)
	require.NoError(t, err)
	require.EqualValues(t, 1000, seq.Total)
	require.EqualValues(t, 1, seq.Malformed)
	require.EqualValues(t, 600, seq.ByStatus[200])
	require.EqualValues(t, 200, seq.ByStatus[404])

	chunked, err := analyze.Chunked(path, 4, parse.ParseFast)
	require.NoError(t, err)
	requireSameStats(t, seq, chunked)

	piped, err := analyze.Pipeline(strings.NewReader(data), 4, parse.ParseFast)
	require.NoError(t, err)
	requireSameStats(t, seq, piped)
}

func requireSameStats(t *testing.T, want, got *analyze.Stats) {
	t.Helper()
	require.Equal(t, want.Total, got.Total)
	require.Equal(t, want.Malformed, got.Malformed)
	require.Equal(t, want.ByStatus, got.ByStatus)
	require.Equal(t, want.ByIP, got.ByIP)
	require.Equal(t, want.ByPath, got.ByPath)
	require.Equal(t, want.Percentile(95), got.Percentile(95))
}

func TestTopN(t *testing.T) {
	top := analyze.TopN(map[string]int64{"a": 5, "b": 9, "c": 1}, 2)
	require.Equal(t, []analyze.Pair[string]{{"b", 9}, {"a", 5}}, top)

	// same generic function, different key type:
	topStatus := analyze.TopN(map[int]int64{200: 100, 404: 7}, 1)
	require.Equal(t, []analyze.Pair[int]{{200, 100}}, topStatus)
}
