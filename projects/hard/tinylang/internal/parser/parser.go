// Package parser is a Pratt (precedence-climbing) parser: every token kind may
// have a prefix rule (how it starts an expression) and an infix rule (how it
// continues one, with a binding power); expr(bp) loops while the next token
// binds tighter than bp.
//
// Syntax errors are raised with panic(*SyntaxError) and recovered once per
// statement (text/template does the same): that removes error plumbing from
// every rule, and a typo in one statement still lets the next one be checked.
// stmt always consumes at least one token, so parsing cannot loop forever.
package parser

import (
	"errors"
	"fmt"
	"slices"
	"strconv"

	"tinylang/internal/ast"
	"tinylang/internal/lexer"
	"tinylang/internal/token"
)

type SyntaxError struct {
	Pos token.Pos
	Msg string
}

func (e *SyntaxError) Error() string { return fmt.Sprintf("%v: syntax error: %s", e.Pos, e.Msg) }

const (
	maxNesting = 200 // bounds recursion so hostile input cannot exhaust the Go stack
	unary      = 7   // binding power of prefix operators, between * and call/index
)

type rule struct {
	bp    int
	infix func(left ast.Expr) ast.Expr
}

type parser struct {
	toks   []token.Token
	i      int
	depth  int
	errs   []error
	prefix map[token.Kind]func() ast.Expr
	infix  map[token.Kind]rule
}

// Parse returns the program and every syntax error found, as an errors.Join of *SyntaxError.
func Parse(src string) (*ast.Program, error) {
	p := &parser{toks: lexer.Lex(src)}
	p.prefix = map[token.Kind]func() ast.Expr{
		token.IDENT: p.literal, token.INT: p.literal, token.STRING: p.literal,
		token.TRUE: p.literal, token.FALSE: p.literal, token.NULL: p.literal,
		token.MINUS: p.unary, token.BANG: p.unary, token.LPAREN: p.group,
		token.LBRACKET: p.array, token.FN: p.fn, token.IF: p.ifExpr,
	}
	p.infix = map[token.Kind]rule{ // binding powers: || && == != < > <= >= + - * / %
		token.OR: {1, p.binary}, token.AND: {2, p.binary},
		token.EQ: {3, p.binary}, token.NE: {3, p.binary},
		token.LT: {4, p.binary}, token.GT: {4, p.binary}, token.LE: {4, p.binary}, token.GE: {4, p.binary},
		token.PLUS: {5, p.binary}, token.MINUS: {5, p.binary},
		token.STAR: {6, p.binary}, token.SLASH: {6, p.binary}, token.PERCENT: {6, p.binary},
		token.LPAREN: {8, p.call}, token.LBRACKET: {8, p.index},
	}
	return &ast.Program{Block: p.stmts(token.EOF)}, errors.Join(p.errs...)
}

func (p *parser) tok() token.Token     { return p.toks[min(p.i, len(p.toks)-1)] }
func (p *parser) at(k token.Kind) bool { return p.tok().Kind == k }

func (p *parser) next() token.Token {
	t := p.tok()
	p.i++
	return t
}

func (p *parser) accept(k token.Kind) bool {
	ok := p.at(k)
	if ok {
		p.i++
	}
	return ok
}

func (p *parser) fail(t token.Token, want string) {
	msg := "unexpected " + t.String()
	switch {
	case t.Kind == token.ILLEGAL:
		msg = t.Lit
	case want != "":
		msg = fmt.Sprintf("expected %s, found %v", want, t)
	}
	panic(&SyntaxError{t.Pos, msg})
}

func (p *parser) expect(k token.Kind) token.Token {
	if !p.at(k) {
		p.fail(p.tok(), strconv.Quote(k.String()))
	}
	return p.next()
}

func (p *parser) enter() {
	if p.depth++; p.depth > maxNesting {
		panic(&SyntaxError{p.tok().Pos, "nesting too deep"})
	}
}

// stmts parses statements up to the end token and notes the names they `let`.
func (p *parser) stmts(end token.Kind) (b ast.Block) {
	for !p.at(end) && !p.at(token.EOF) {
		if s := p.stmt(); s != nil {
			b.Stmts = append(b.Stmts, s)
			if l, ok := s.(*ast.Let); ok && !slices.Contains(b.Lets, l.Name) {
				b.Lets = append(b.Lets, l.Name)
			}
		}
	}
	return b
}

// stmt parses one statement; on a syntax error it records it, skips to a
// plausible statement boundary and returns nil.
func (p *parser) stmt() (s ast.Stmt) {
	start, depth := p.i, p.depth
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		se, ok := r.(*SyntaxError)
		if !ok {
			panic(r)
		}
		p.errs = append(p.errs, se)
		p.depth, s = depth, nil
		p.sync()
		if p.i == start {
			p.i++
		}
	}()
	p.enter()
	s = p.statement()
	p.depth--
	return s
}

// sync skips to the next statement boundary, stepping over whole {...} groups
// so the tail of a broken function body does not cause follow-up errors.
func (p *parser) sync() {
	for depth := 0; !p.at(token.EOF); p.i++ {
		switch k := p.tok().Kind; {
		case k == token.LBRACE:
			depth++
		case k == token.RBRACE && depth > 0:
			depth--
		case depth > 0:
		case k == token.SEMI:
			p.i++
			return
		case k == token.RBRACE || k == token.LET || k == token.WHILE || k == token.RETURN:
			return
		}
	}
}

func (p *parser) statement() ast.Stmt {
	var s ast.Stmt
	switch t := p.tok(); t.Kind {
	case token.LET:
		p.i++
		name := p.expect(token.IDENT).Lit
		p.expect(token.ASSIGN)
		s = &ast.Let{Name: name, Value: p.expr(0)}
	case token.RETURN:
		p.i++
		var v ast.Expr = &ast.NullLit{}
		if !p.at(token.SEMI) && !p.at(token.RBRACE) && !p.at(token.EOF) {
			v = p.expr(0)
		}
		s = &ast.Return{Value: v}
	case token.WHILE:
		p.i++
		s = &ast.While{At: t.Pos, Cond: p.expr(0), Body: p.block()}
	default:
		x := p.expr(0)
		eq := p.tok()
		if !p.accept(token.ASSIGN) {
			s = &ast.ExprStmt{X: x}
			break
		}
		id, ok := x.(*ast.Ident)
		if !ok {
			panic(&SyntaxError{eq.Pos, "cannot assign to " + x.String()})
		}
		s = &ast.Assign{Target: id, Value: p.expr(0)}
	}
	p.accept(token.SEMI)
	return s
}

func (p *parser) expr(bp int) ast.Expr {
	tok := p.tok()
	prefix := p.prefix[tok.Kind]
	if prefix == nil {
		p.fail(tok, "")
	}
	p.enter()
	left := prefix()
	for p.infix[p.tok().Kind].bp > bp {
		left = p.infix[p.tok().Kind].infix(left)
	}
	p.depth--
	return left
}

func (p *parser) literal() ast.Expr {
	t := p.next()
	switch t.Kind {
	case token.IDENT:
		return &ast.Ident{At: t.Pos, Name: t.Lit}
	case token.INT:
		n, _ := strconv.ParseInt(t.Lit, 10, 64) // the lexer already rejected overflow
		return &ast.IntLit{Val: n}
	case token.STRING:
		return &ast.StrLit{Val: t.Lit}
	case token.NULL:
		return &ast.NullLit{}
	}
	return &ast.BoolLit{Val: t.Kind == token.TRUE}
}

// Prefix operators are sugar too: -x is 0 - x, and !x is `if x { false } else { true }`.
func (p *parser) unary() ast.Expr {
	t := p.next()
	x := p.expr(unary)
	if t.Kind == token.BANG {
		return &ast.If{Cond: x, Then: blockOf(&ast.BoolLit{}), Else: blockOf(&ast.BoolLit{Val: true})}
	}
	return &ast.Infix{At: t.Pos, Op: token.MINUS, L: &ast.IntLit{}, R: x}
}

func (p *parser) group() ast.Expr {
	p.i++
	x := p.expr(0)
	p.expect(token.RPAREN)
	return x
}

func (p *parser) array() ast.Expr {
	p.i++
	return &ast.Array{Elems: p.exprList(token.RBRACKET)}
}

func (p *parser) exprList(end token.Kind) (xs []ast.Expr) {
	for !p.at(end) {
		xs = append(xs, p.expr(0))
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(end)
	return xs
}

func (p *parser) fn() ast.Expr {
	p.i++
	p.expect(token.LPAREN)
	var params []string
	for !p.at(token.RPAREN) {
		id := p.expect(token.IDENT)
		if slices.Contains(params, id.Lit) {
			p.fail(id, "") // two parameters would need one slot
		}
		params = append(params, id.Lit)
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN)
	return &ast.Fn{Params: params, Body: p.block()}
}

func (p *parser) ifExpr() ast.Expr {
	p.i++
	n := &ast.If{Cond: p.expr(0), Then: p.block()}
	if p.accept(token.ELSE) {
		if p.at(token.IF) { // else if: an else block holding just the nested if
			n.Else = blockOf(p.ifExpr())
		} else {
			n.Else = p.block()
		}
	}
	return n
}

func (p *parser) block() *ast.Block {
	p.expect(token.LBRACE)
	b := p.stmts(token.RBRACE)
	p.expect(token.RBRACE)
	return &b
}

func blockOf(x ast.Expr) *ast.Block { return &ast.Block{Stmts: []ast.Stmt{&ast.ExprStmt{X: x}}} }

// && and || are sugar over if, so neither engine needs to know about them:
// a && b is `if a { b } else { false }`, a || b is `if a { true } else { b }`.
func (p *parser) binary(left ast.Expr) ast.Expr {
	t := p.next()
	right := p.expr(p.infix[t.Kind].bp)
	switch t.Kind {
	case token.AND:
		return &ast.If{Cond: left, Then: blockOf(right), Else: blockOf(&ast.BoolLit{})}
	case token.OR:
		return &ast.If{Cond: left, Then: blockOf(&ast.BoolLit{Val: true}), Else: blockOf(right)}
	}
	return &ast.Infix{At: t.Pos, Op: t.Kind, L: left, R: right}
}

func (p *parser) call(left ast.Expr) ast.Expr {
	t := p.next()
	return &ast.Call{At: t.Pos, Fn: left, Args: p.exprList(token.RPAREN)}
}

func (p *parser) index(left ast.Expr) ast.Expr {
	t := p.next()
	i := p.expr(0)
	p.expect(token.RBRACKET)
	return &ast.Index{At: t.Pos, X: left, I: i}
}
