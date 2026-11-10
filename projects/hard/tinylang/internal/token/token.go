// Package token defines tinylang's vocabulary: token kinds and source positions.
package token

import (
	"fmt"
	"strconv"
	"strings"
)

type Kind uint8

const (
	ILLEGAL Kind = iota
	EOF
	IDENT
	INT
	STRING
	ASSIGN
	PLUS
	MINUS
	STAR
	SLASH
	PERCENT
	BANG
	LT
	GT
	LE
	GE
	EQ
	NE
	AND
	OR
	COMMA
	SEMI
	LPAREN
	RPAREN
	LBRACE
	RBRACE
	LBRACKET
	RBRACKET
	LET
	FN
	IF
	ELSE
	WHILE
	RETURN
	TRUE
	FALSE
	NULL
)

// names is indexed by Kind: keep it in the same order as the constants.
var names = strings.Fields(`illegal EOF identifier integer string
	= + - * / % ! < > <= >= == != && || , ; ( ) { } [ ]
	let fn if else while return true false null`)

func (k Kind) String() string { return names[k] }

var byText = map[string]Kind{}

func init() {
	for k := ASSIGN; k <= NULL; k++ {
		byText[names[k]] = k
	}
}

// Lookup maps keyword and operator text to its Kind; unknown text gives ILLEGAL.
func Lookup(text string) Kind { return byText[text] }

type Pos struct{ Line, Col int }

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Token.Lit is the source text; for STRING the decoded value, for ILLEGAL a message.
type Token struct {
	Kind Kind
	Lit  string
	Pos  Pos
}

func (t Token) String() string {
	if t.Kind == EOF {
		return "end of input"
	}
	return strconv.Quote(t.Lit)
}
