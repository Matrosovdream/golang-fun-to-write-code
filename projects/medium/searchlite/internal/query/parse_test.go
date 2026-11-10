package query

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// show prints a tree fully parenthesised, in a form Parse accepts again.
func show(n Node) string {
	switch n := n.(type) {
	case Term:
		return n.Text
	case Phrase:
		return `"` + n.Text + `"`
	case And:
		return fmt.Sprintf("(%s AND %s)", show(n.L), show(n.R))
	case Or:
		return fmt.Sprintf("(%s OR %s)", show(n.L), show(n.R))
	case Not:
		return "NOT " + show(n.X)
	}
	return "?"
}

func TestParse(t *testing.T) {
	tests := []struct{ in, want string }{
		{"go", "go"},
		{"a b", "(a AND b)"},
		{"a AND b", "(a AND b)"},
		{"a OR b", "(a OR b)"},
		{"a b OR c", "((a AND b) OR c)"},
		{"a OR b c", "(a OR (b AND c))"},
		{"a b c", "((a AND b) AND c)"},
		{"a OR b OR c", "((a OR b) OR c)"},
		{"(a OR b) c", "((a OR b) AND c)"},
		{"((a))", "a"},
		{"-a", "NOT a"},
		{"NOT a", "NOT a"},
		{"a -b", "(a AND NOT b)"},
		{"a AND NOT b", "(a AND NOT b)"},
		{"--a", "NOT NOT a"},
		{"-(a OR b)", "NOT (a OR b)"},
		{"- a", "NOT a"},
		{`"memory model"`, `"memory model"`},
		{`goroutine AND "memory model" -mutex`, `((goroutine AND "memory model") AND NOT mutex)`},
		{`-"a b" c`, `(NOT "a b" AND c)`},
		{"well-known", "well-known"},
		{"and or not", "((and AND or) AND not)"},
		{`a"b c"d`, `((a AND "b c") AND d)`},
		{"  a\t\n b  ", "(a AND b)"},
		{"naïve café", "(naïve AND café)"},
	}
	for _, tt := range tests {
		n, err := Parse(tt.in)
		require.NoError(t, err, tt.in)
		require.Equal(t, tt.want, show(n), tt.in)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		in   string
		off  int
		part string
	}{
		{"", 0, "empty query"},
		{"   ", 3, "empty query"},
		{"a AND", 5, "end of query"},
		{"AND a", 0, `"AND"`},
		{"a OR OR b", 5, `"OR"`},
		{"a OR", 4, "end of query"},
		{"a )", 2, `")"`},
		{"(a", 0, `unclosed "("`},
		{"a (b OR (c d)", 2, `unclosed "("`},
		{"()", 1, `")"`},
		{`"abc`, 0, "unterminated phrase"},
		{`a "b c`, 2, "unterminated phrase"},
		{"a -", 3, "end of query"},
		{"NOT", 3, "end of query"},
		{"a NOT AND b", 6, `"AND"`},
		{"é )", 3, `")"`}, // offsets count bytes, not runes
	}
	for _, tt := range tests {
		_, err := Parse(tt.in)
		var pe *ParseError
		require.ErrorAs(t, err, &pe, tt.in)
		require.Equal(t, tt.off, pe.Off, tt.in)
		require.Contains(t, pe.Msg, tt.part, tt.in)
		require.Contains(t, err.Error(), fmt.Sprintf("offset %d", tt.off))
	}
}

func TestPositive(t *testing.T) {
	n, err := Parse(`a AND (b OR "c d") -e NOT (f g)`)
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b", "c d"}, Positive(n))
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "a b OR c", `"x y" -z`, "(a OR (b", `a"b`, "NOT NOT -", "é)"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := Parse(s)
		if err != nil {
			var pe *ParseError
			require.True(t, errors.As(err, &pe))
			require.True(t, pe.Off >= 0 && pe.Off <= len(s), "offset %d outside %q", pe.Off, s)
			return
		}
		again, err := Parse(show(n))
		require.NoError(t, err, show(n))
		require.Equal(t, n, again)
		Positive(n)
	})
}
