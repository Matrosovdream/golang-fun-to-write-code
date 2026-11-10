package main

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"tinylang"
	"tinylang/internal/lexer"
	"tinylang/internal/object"
	"tinylang/internal/token"
)

// repl reads input until its brackets balance, runs it, and echoes any value.
// Variables persist across entries because the engine keeps its global scope.
func repl(in io.Reader, out io.Writer, e tinylang.Engine) error {
	sc := bufio.NewScanner(in)
	var buf strings.Builder
	for fmt.Fprint(out, ">> "); sc.Scan(); {
		buf.WriteString(sc.Text() + "\n")
		if !balanced(buf.String()) {
			fmt.Fprint(out, ".. ")
			continue
		}
		src := buf.String()
		buf.Reset()
		if v, err := tinylang.Run(e, src); err != nil {
			fmt.Fprintln(out, err)
		} else if v != nil {
			fmt.Fprintln(out, object.Inspect(v))
		}
		fmt.Fprint(out, ">> ")
	}
	return sc.Err()
}

func balanced(src string) bool {
	depth := 0
	for _, t := range lexer.Lex(src) {
		switch t.Kind {
		case token.LPAREN, token.LBRACE, token.LBRACKET:
			depth++
		case token.RPAREN, token.RBRACE, token.RBRACKET:
			depth--
		}
	}
	return depth <= 0
}
