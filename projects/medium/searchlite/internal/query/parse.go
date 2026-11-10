// Package query parses the search language into a tree.
//
// Grammar, loosest binding first. Adjacent operands mean AND, so `a b OR c`
// is `(a AND b) OR c`:
//
//	expr  := or
//	or    := and ('OR' and)*
//	and   := unary (['AND'] unary)*
//	unary := ('-' | 'NOT') unary | atom
//	atom  := WORD | '"' words '"' | '(' expr ')'
//
// AND, OR and NOT are operators only in capitals. A '-' negates wherever a
// term can start; inside a word (well-known) it is an ordinary character.
// Every ParseError carries the byte offset of the problem.
package query

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Node any

type (
	Term   struct{ Text string } // one bare word
	Phrase struct{ Text string } // the words inside quotes
	And    struct{ L, R Node }
	Or     struct{ L, R Node }
	Not    struct{ X Node }
)

type ParseError struct {
	Off int
	Msg string
}

func (e *ParseError) Error() string { return fmt.Sprintf("query: offset %d: %s", e.Off, e.Msg) }

// Positive returns the text of every Term and Phrase not under a Not: the
// words a matching document should be highlighted for.
func Positive(n Node) []string {
	switch n := n.(type) {
	case Term:
		return []string{n.Text}
	case Phrase:
		return []string{n.Text}
	case And:
		return append(Positive(n.L), Positive(n.R)...)
	case Or:
		return append(Positive(n.L), Positive(n.R)...)
	}
	return nil
}

// kind is "(", ")", "-", "AND", "OR", "NOT", "word", "phrase" or "eof".
type token struct {
	kind, text string
	off        int
}

func lex(s string) ([]token, error) {
	var toks []token
	for i := 0; i < len(s); {
		c, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case unicode.IsSpace(c):
			i += size
		case strings.ContainsRune("()-", c):
			toks = append(toks, token{string(c), string(c), i})
			i++
		case c == '"':
			n := strings.IndexByte(s[i+1:], '"')
			if n < 0 {
				return nil, &ParseError{i, "unterminated phrase"}
			}
			toks = append(toks, token{"phrase", s[i+1 : i+1+n], i})
			i += n + 2
		default:
			n := strings.IndexFunc(s[i:], func(r rune) bool { return unicode.IsSpace(r) || strings.ContainsRune(`()"`, r) })
			if n < 0 {
				n = len(s) - i
			}
			kind := "word"
			if w := s[i : i+n]; w == "AND" || w == "OR" || w == "NOT" {
				kind = w
			}
			toks = append(toks, token{kind, s[i : i+n], i})
			i += n
		}
	}
	return append(toks, token{"eof", "", len(s)}), nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if t.kind != "eof" {
		p.i++
	}
	return t
}

func Parse(s string) (Node, error) {
	toks, err := lex(s)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	if p.peek().kind == "eof" {
		return nil, &ParseError{len(s), "empty query"}
	}
	n, err := p.or()
	if err != nil {
		return nil, err
	}
	// or() stops only at the end or at a ')' that has no matching '('.
	if t := p.peek(); t.kind != "eof" {
		return nil, unexpected(t)
	}
	return n, nil
}

func (p *parser) or() (Node, error) {
	n, err := p.and()
	for err == nil && p.peek().kind == "OR" {
		p.next()
		var r Node
		if r, err = p.and(); err == nil {
			n = Or{n, r}
		}
	}
	return n, err
}

func (p *parser) and() (Node, error) {
	n, err := p.unary()
	for err == nil {
		switch p.peek().kind {
		case "AND":
			p.next()
		case "word", "phrase", "(", "NOT", "-": // implicit AND
		default:
			return n, nil
		}
		var r Node
		if r, err = p.unary(); err == nil {
			n = And{n, r}
		}
	}
	return n, err
}

func (p *parser) unary() (Node, error) {
	switch t := p.next(); t.kind {
	case "-", "NOT":
		x, err := p.unary()
		return Not{x}, err
	case "word":
		return Term{t.text}, nil
	case "phrase":
		return Phrase{t.text}, nil
	case "(":
		n, err := p.or()
		if err != nil {
			return nil, err
		}
		if p.next().kind == "eof" {
			return nil, &ParseError{t.off, `unclosed "("`}
		}
		return n, nil
	default:
		return nil, unexpected(t)
	}
}

func unexpected(t token) error {
	if t.kind == "eof" {
		return &ParseError{t.off, "unexpected end of query"}
	}
	return &ParseError{t.off, fmt.Sprintf("unexpected %q", t.text)}
}
