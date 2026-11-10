package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang/internal/ast"
)

func TestParsePrinted(t *testing.T) {
	tests := []struct{ src, want string }{
		{"1 + 2 * 3", "(1 + (2 * 3));"},
		{"1 * 2 + 3", "((1 * 2) + 3);"},
		{"a - b - c", "((a - b) - c);"},
		{"-a * b", "((0 - a) * b);"},
		{"!a == b", "(if a { false; } else { true; } == b);"},
		{"- -a", "(0 - (0 - a));"},
		{"a < b == c > d", "((a < b) == (c > d));"},
		{"a || b && c == d", "if a { true; } else { if b { (c == d); } else { false; }; };"},
		{"(1 + 2) * 3", "((1 + 2) * 3);"},
		{"a + f(b, c)[0] * 2", "(a + (f(b, c)[0] * 2));"},
		{"f(1)(2)", "f(1)(2);"},
		{"-f(1)", "(0 - f(1));"},
		{"[1, [2], f()]", "[1, [2], f()];"},
		{"[1, 2,]", "[1, 2];"},
		{`"a\n" + "b"`, `("a\n" + "b");`},
		{"true && null", "if true { null; } else { false; };"},
		{"let x = 1; let y = x", "let x = 1;\nlet y = x;"},
		{"x = x + 1", "x = (x + 1);"},
		{"return", "return null;"},
		{"return 1 + 2;", "return (1 + 2);"},
		{"fn(a, b) { a + b }", "fn(a, b) { (a + b); };"},
		{"let f = fn() {}", "let f = fn() {  };"},
		{"if a { b }", "if a { b; };"},
		{"if a { b } else { c }", "if a { b; } else { c; };"},
		{"if a { 1 } else if b { 2 } else { 3 }", "if a { 1; } else { if b { 2; } else { 3; }; };"},
		{"while i < 3 { i = i + 1; }", "while (i < 3) { i = (i + 1); }"},
		{"while a { }; b", "while a {  }\nb;"},
		{"fn(x) { x }(5)", "fn(x) { x; }(5);"},
		{"1 // comment\n+ 2", "(1 + 2);"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			prog, err := Parse(tt.src)
			require.NoError(t, err)
			require.Equal(t, tt.want, prog.String())
		})
	}
}

func TestLetsAreHoisted(t *testing.T) {
	prog, err := Parse("let a = 1; let b = fn() { let c = 1; let c = 2; }; let a = 3;")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "b"}, prog.Lets)
	fn := prog.Stmts[1].(*ast.Let).Value.(*ast.Fn)
	require.Equal(t, []string{"c"}, fn.Body.Lets)
}

func TestSyntaxErrors(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{"missing operand", "1 +", []string{"1:4: syntax error: unexpected end of input"}},
		{"missing paren", "(1 + 2", []string{`1:7: syntax error: expected ")", found end of input`}},
		{"missing name", "let = 5", []string{`1:5: syntax error: expected "identifier", found "="`}},
		{"missing assign", "let x 5", []string{`1:7: syntax error: expected "=", found "5"`}},
		{"bad assign target", "1 = 2", []string{"1:3: syntax error: cannot assign to 1"}},
		{"element assignment", "a[0] = 2", []string{"1:6: syntax error: cannot assign to a[0]"}},
		{"duplicate param", "fn(a, a) {}", []string{`1:7: syntax error: unexpected "a"`}},
		{"unclosed block", "fn() { 1", []string{`1:9: syntax error: expected "}", found end of input`}},
		{"stray brace", "}", []string{`1:1: syntax error: unexpected "}"`}},
		{"stray semicolon", ";", []string{`1:1: syntax error: unexpected ";"`}},
		{"illegal char", "let x = 5 @", []string{"1:11: syntax error: illegal character '@'"}},
		{"unterminated string", `let s = "abc`, []string{"1:9: syntax error: unterminated string"}},
		{"else without if", "else { 1 }", []string{`1:1: syntax error: unexpected "else"`}},
		{"if needs block", "if a 1", []string{`1:6: syntax error: expected "{", found "1"`}},
		{"one error per statement, next ones still reported", "let x = ;\nlet y = 2 +;\nprint(x)\nlet z = )", []string{
			`1:9: syntax error: unexpected ";"`,
			`2:12: syntax error: unexpected ";"`,
			`4:9: syntax error: unexpected ")"`,
		}},
		{"error inside a function body does not hide the next statement", "let f = fn() { let = 1; return 2 }\n1 +", []string{
			`1:20: syntax error: expected "identifier", found "="`,
			`2:4: syntax error: unexpected end of input`,
		}},
		{"nesting limit", strings.Repeat("(", 300), []string{"1:200: syntax error: nesting too deep"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.src)
			require.Error(t, err)
			var got []string
			for e := range strings.SplitSeq(err.Error(), "\n") {
				got = append(got, e)
			}
			require.Equal(t, tt.want, got)
			var se *SyntaxError
			require.True(t, errors.As(err, &se))
			require.GreaterOrEqual(t, se.Pos.Line, 1)
		})
	}
}

// Whatever the input, the parser terminates; an accepted program prints to
// source that parses back to the same printed form.
func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "1 + 2 * 3", "let f = fn(n) { if n < 2 { n } else { f(n-1) + f(n-2) } }; f(10)",
		"while i < 3 { i = i + 1 }", `"a\tb" + "c"`, "a[0] = 3", "if a { } else if b { } else { }",
		"let = ;", "}{", "fn(a, a) {}", "\"", "\xff", "((((", "return;", "a[", "f(,)", "- - - 1",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		prog, err := Parse(src)
		if err != nil {
			for _, e := range err.(interface{ Unwrap() []error }).Unwrap() {
				se, ok := e.(*SyntaxError)
				require.True(t, ok)
				require.GreaterOrEqual(t, se.Pos.Line, 1)
				require.GreaterOrEqual(t, se.Pos.Col, 1)
			}
			return
		}
		if len(src) > 2000 {
			return // printing nests Sprintf calls: quadratic on long operator chains
		}
		printed := prog.String()
		again, err := Parse(printed)
		if err != nil && strings.Contains(err.Error(), "nesting too deep") {
			return // printing parenthesizes every operator, so a long a+b+c+... chain nests deeper on re-parse
		}
		require.NoError(t, err, printed)
		require.Equal(t, printed, again.String())
	})
}
