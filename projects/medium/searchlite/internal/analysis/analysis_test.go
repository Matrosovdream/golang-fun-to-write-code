package analysis

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStem(t *testing.T) {
	tests := []struct{ in, want string }{
		{"goroutines", "goroutin"}, {"goroutine", "goroutin"},
		{"channels", "channel"}, {"queries", "query"}, {"classes", "class"}, {"class", "class"},
		{"mutexes", "mutex"}, {"analysis", "analysi"},
		{"running", "run"}, {"scheduling", "schedul"}, {"scheduled", "schedul"}, {"schedule", "schedul"},
		{"installing", "install"}, {"embedded", "embed"}, {"embed", "emb"},
		{"quickly", "quick"}, {"string", "string"}, {"thing", "thing"},
		{"go", "go"}, {"gos", "gos"}, {"", ""},
	}
	for _, tt := range tests {
		require.Equal(t, tt.want, Stem(tt.in), tt.in)
	}
}

func terms(a *Analyzer, text string) []string {
	var out []string
	for t := range a.Tokens(text) {
		out = append(out, t.Term)
	}
	return out
}

func TestDefaultPipeline(t *testing.T) {
	require.Equal(t,
		[]string{"goroutin", "run", "memory", "model"},
		terms(Default(), "The Goroutines are RUNNING, in a Memory-Model!"))
	require.Empty(t, terms(Default(), "  ... the of and ---"))
}

func TestPositionsAndOffsets(t *testing.T) {
	text := "State of the art: café 42"
	var got []Token
	for tok := range Default().Tokens(text) {
		got = append(got, tok)
	}
	// "of" and "the" are dropped but still occupy positions 1 and 2.
	require.Equal(t, []Token{
		{"stat", 0, 0, 5}, {"art", 3, 13, 16}, {"café", 4, 18, 23}, {"42", 5, 24, 26},
	}, got)
	var raw []string
	for _, tok := range got {
		raw = append(raw, text[tok.Start:tok.End])
	}
	require.Equal(t, []string{"State", "art", "café", "42"}, raw)
}

func TestStagesCompose(t *testing.T) {
	shout := New(Map(strings.ToUpper), Drop(func(w string) bool { return len(w) < 3 }))
	require.Equal(t, []string{"GOROUTINES", "RUN"}, terms(shout, "goroutines go run"))
}

func TestTokensIsLazy(t *testing.T) {
	var first Token
	for tok := range Default().Tokens("one two three") {
		first = tok
		break
	}
	require.Equal(t, Token{"one", 0, 0, 3}, first)
}
