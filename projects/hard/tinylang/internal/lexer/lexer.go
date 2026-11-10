// Package lexer turns source text into tokens. It cannot fail: bad input
// becomes ILLEGAL tokens whose Lit says what is wrong, and every call consumes
// at least one byte, so the parser can never be starved or looped by it.
package lexer

import (
	"fmt"
	"strconv"
	"unicode/utf8"

	"tinylang/internal/token"
)

type lexer struct {
	src       string
	i         int
	line, col int
}

// Lex returns all tokens of src, always ending with EOF.
func Lex(src string) []token.Token {
	l := &lexer{src: src, line: 1, col: 1}
	var toks []token.Token
	for l.skip(); l.i < len(src); l.skip() {
		toks = append(toks, l.token())
	}
	return append(toks, token.Token{Kind: token.EOF, Pos: l.pos()})
}

func (l *lexer) pos() token.Pos { return token.Pos{Line: l.line, Col: l.col} }

// adv consumes one byte; columns count characters, not bytes.
func (l *lexer) adv() {
	if l.i >= len(l.src) {
		return
	}
	if c := l.src[l.i]; c == '\n' {
		l.line, l.col = l.line+1, 1
	} else if c&0xC0 != 0x80 {
		l.col++
	}
	l.i++
}

func (l *lexer) while(ok func(c byte) bool) {
	for l.i < len(l.src) && ok(l.src[l.i]) {
		l.adv()
	}
}

func (l *lexer) skip() {
	l.while(func(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' })
	if l.i+1 < len(l.src) && l.src[l.i:l.i+2] == "//" {
		l.while(func(c byte) bool { return c != '\n' })
		l.skip()
	}
}

func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isLetter(c byte) bool { return c == '_' || c|0x20 >= 'a' && c|0x20 <= 'z' }

func (l *lexer) token() token.Token {
	start, pos := l.i, l.pos()
	tok := func(k token.Kind, lit string) token.Token { return token.Token{Kind: k, Lit: lit, Pos: pos} }
	switch c := l.src[l.i]; {
	case isLetter(c):
		l.while(func(c byte) bool { return isLetter(c) || isDigit(c) })
		if k := token.Lookup(l.src[start:l.i]); k != token.ILLEGAL {
			return tok(k, l.src[start:l.i])
		}
		return tok(token.IDENT, l.src[start:l.i])
	case isDigit(c):
		l.while(isDigit)
		if _, err := strconv.ParseInt(l.src[start:l.i], 10, 64); err != nil {
			return tok(token.ILLEGAL, "integer literal too large")
		}
		return tok(token.INT, l.src[start:l.i])
	case c == '"':
		l.adv()
		for l.i < len(l.src) && l.src[l.i] != '"' && l.src[l.i] != '\n' {
			if l.src[l.i] == '\\' {
				l.adv()
			}
			l.adv()
		}
		if l.i >= len(l.src) || l.src[l.i] != '"' {
			return tok(token.ILLEGAL, "unterminated string")
		}
		l.adv()
		s, err := strconv.Unquote(l.src[start:l.i])
		if err != nil {
			return tok(token.ILLEGAL, "invalid string literal")
		}
		return tok(token.STRING, s)
	}
	for n := min(2, len(l.src)-l.i); n > 0; n-- { // longest operator first: "==" before "="
		if k := token.Lookup(l.src[l.i : l.i+n]); k != token.ILLEGAL {
			for range n {
				l.adv()
			}
			return tok(k, l.src[start:l.i])
		}
	}
	r, n := utf8.DecodeRuneInString(l.src[l.i:])
	for range n {
		l.adv()
	}
	return tok(token.ILLEGAL, fmt.Sprintf("illegal character %q", r))
}
