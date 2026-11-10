package lexer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang/internal/token"
)

func TestLex(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string // kind, plus ":text" for kinds that carry text
	}{
		{"operators", "= == != <= >= < > && || ! % + - * /", []string{"=", "==", "!=", "<=", ">=", "<", ">", "&&", "||", "!", "%", "+", "-", "*", "/", "EOF"}},
		{"punctuation", "( ) { } [ ] , ;", []string{"(", ")", "{", "}", "[", "]", ",", ";", "EOF"}},
		{"keywords vs idents", "let letter fn fnord if iffy", []string{"let", "identifier:letter", "fn", "identifier:fnord", "if", "identifier:iffy", "EOF"}},
		{"numbers", "0 42 007", []string{"integer:0", "integer:42", "integer:007", "EOF"}},
		{"strings decode escapes", `"a\tb" "q\"q" ""`, []string{"string:a\tb", `string:q"q`, "string:", "EOF"}},
		{"comments", "1 // two\n3", []string{"integer:1", "integer:3", "EOF"}},
		{"slash alone", "6 / 3", []string{"integer:6", "/", "integer:3", "EOF"}},
		{"illegal char", "1 @ 2", []string{"integer:1", "illegal:illegal character '@'", "integer:2", "EOF"}},
		{"lone ampersand", "a & b", []string{"identifier:a", "illegal:illegal character '&'", "identifier:b", "EOF"}},
		{"unterminated string", `"abc`, []string{"illegal:unterminated string", "EOF"}},
		{"newline in string", "\"ab\ncd\"", []string{"illegal:unterminated string", "identifier:cd", "illegal:unterminated string", "EOF"}},
		{"bad escape", `"\q"`, []string{"illegal:invalid string literal", "EOF"}},
		{"int overflow", "99999999999999999999", []string{"illegal:integer literal too large", "EOF"}},
		{"stray utf8", "é", []string{"illegal:illegal character 'é'", "EOF"}},
		{"invalid utf8", "\xff", []string{"illegal:illegal character '�'", "EOF"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, tok := range Lex(tt.src) {
				s := tok.Kind.String()
				if tok.Kind == token.ILLEGAL || tok.Kind >= token.IDENT && tok.Kind <= token.STRING {
					s += ":" + tok.Lit
				}
				got = append(got, s)
			}
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPositions(t *testing.T) {
	// Columns count characters: "é" is two bytes but one column.
	toks := Lex("let x = 1;\n  fn(é) \"héllo\" y")
	var got []string
	for _, tok := range toks {
		got = append(got, tok.Pos.String())
	}
	require.Equal(t, []string{"1:1", "1:5", "1:7", "1:9", "1:10", "2:3", "2:5", "2:6", "2:7", "2:9", "2:17", "2:18"}, got)
}
