// Package ast is the syntax tree as two sum types: Expr and Stmt. Each is an
// interface with an unexported marker method, so only this package can add
// variants, and the engines' type switches can be checked for completeness.
//
// String prints a node back as valid tinylang source with every operator
// parenthesized, which makes precedence visible in tests and lets the fuzzer
// check that parse(print(parse(x))) is stable.
package ast

import (
	"fmt"
	"strconv"
	"strings"

	"tinylang/internal/token"
)

type Expr interface {
	fmt.Stringer
	expr()
}

type Stmt interface {
	fmt.Stringer
	stmt()
}

type (
	Ident struct {
		At   token.Pos
		Name string
	}
	IntLit  struct{ Val int64 }
	StrLit  struct{ Val string }
	BoolLit struct{ Val bool }
	NullLit struct{}
	Array   struct{ Elems []Expr }
	Infix   struct {
		At   token.Pos
		Op   token.Kind
		L, R Expr
	}
	If struct {
		Cond       Expr
		Then, Else *Block // Else may be nil
	}
	Fn struct {
		Params []string
		Body   *Block
	}
	Call struct {
		At   token.Pos
		Fn   Expr
		Args []Expr
	}
	Index struct {
		At   token.Pos
		X, I Expr
	}

	Let struct {
		Name  string
		Value Expr
	}
	Assign struct {
		Target *Ident
		Value  Expr
	}
	Return   struct{ Value Expr }
	ExprStmt struct{ X Expr }
	While    struct {
		At   token.Pos
		Cond Expr
		Body *Block
	}
)

// Block is not a Stmt: braces only appear after if/while/fn. Lets lists the
// names its direct let statements declare; both engines hoist them so the
// whole block sees them (that is what makes mutual recursion work).
type Block struct {
	Stmts []Stmt
	Lets  []string
}

type Program struct{ Block }

func (*Ident) expr()   {}
func (*IntLit) expr()  {}
func (*StrLit) expr()  {}
func (*BoolLit) expr() {}
func (*NullLit) expr() {}
func (*Array) expr()   {}
func (*Infix) expr()   {}
func (*If) expr()      {}
func (*Fn) expr()      {}
func (*Call) expr()    {}
func (*Index) expr()   {}

func (*Let) stmt()      {}
func (*Assign) stmt()   {}
func (*Return) stmt()   {}
func (*While) stmt()    {}
func (*ExprStmt) stmt() {}

func join[T fmt.Stringer](xs []T, sep string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = x.String()
	}
	return strings.Join(parts, sep)
}

func (n *Ident) String() string   { return n.Name }
func (n *IntLit) String() string  { return strconv.FormatInt(n.Val, 10) }
func (n *StrLit) String() string  { return strconv.Quote(n.Val) }
func (n *BoolLit) String() string { return strconv.FormatBool(n.Val) }
func (n *NullLit) String() string { return "null" }
func (n *Array) String() string   { return "[" + join(n.Elems, ", ") + "]" }
func (n *Infix) String() string   { return fmt.Sprintf("(%v %v %v)", n.L, n.Op, n.R) }
func (n *Fn) String() string      { return fmt.Sprintf("fn(%s) %v", strings.Join(n.Params, ", "), n.Body) }
func (n *Call) String() string    { return fmt.Sprintf("%v(%s)", n.Fn, join(n.Args, ", ")) }
func (n *Index) String() string   { return fmt.Sprintf("%v[%v]", n.X, n.I) }

func (n *If) String() string {
	s := fmt.Sprintf("if %v %v", n.Cond, n.Then)
	if n.Else != nil {
		s += " else " + n.Else.String()
	}
	return s
}

func (n *Let) String() string      { return fmt.Sprintf("let %s = %v;", n.Name, n.Value) }
func (n *Assign) String() string   { return fmt.Sprintf("%v = %v;", n.Target, n.Value) }
func (n *While) String() string    { return fmt.Sprintf("while %v %v", n.Cond, n.Body) }
func (n *ExprStmt) String() string { return n.X.String() + ";" }

func (n *Return) String() string { return fmt.Sprintf("return %v;", n.Value) }

func (b *Block) String() string   { return "{ " + join(b.Stmts, " ") + " }" }
func (p *Program) String() string { return join(p.Stmts, "\n") }
