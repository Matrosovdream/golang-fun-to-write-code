// Package compiler turns the AST into bytecode for the stack VM.
//
// Variables are addressed as (scopes up, slot). A compile-time scope is pushed
// exactly where the evaluator creates an environment — the program, each
// function, each block that has `let`s — so the two chains always line up and a
// closure is just "code + the environment it was created in".
package compiler

import (
	"slices"

	"tinylang/internal/ast"
	"tinylang/internal/code"
	"tinylang/internal/object"
	"tinylang/internal/token"
)

type scope struct {
	names []string
	outer *scope
}

var none token.Pos // position of instructions that cannot fail

func (s *scope) declare(names ...string) {
	for _, n := range names {
		if !slices.Contains(s.names, n) {
			s.names = append(s.names, n)
		}
	}
}

// Compiler keeps its global scope between Compile calls, for the REPL.
type Compiler struct {
	global, sc *scope
	fn         *code.Fn
}

func New() *Compiler {
	g := &scope{}
	return &Compiler{global: g, sc: g}
}

func (c *Compiler) Compile(p *ast.Program) *code.Fn {
	c.fn, c.sc = &code.Fn{}, c.global
	c.sc.declare(p.Lets...)
	c.stmts(p.Stmts, true)
	c.emit(code.OpReturn, none)
	c.fn.Slots = len(c.sc.names)
	return c.fn
}

func (c *Compiler) emit(op code.Op, pos token.Pos, args ...int) int {
	in := code.Instr{Op: op}
	if len(args) > 0 {
		in.A = int32(args[0])
	}
	if len(args) > 1 {
		in.B = int32(args[1])
	}
	c.fn.Code = append(c.fn.Code, in)
	c.fn.Pos = append(c.fn.Pos, pos)
	return len(c.fn.Code) - 1
}

// patch points the jump at index j to the next instruction to be emitted.
func (c *Compiler) patch(j int) { c.fn.Code[j].A = int32(len(c.fn.Code)) }

func (c *Compiler) konst(v any) int {
	c.fn.Consts = append(c.fn.Consts, v)
	return len(c.fn.Consts) - 1
}

func (c *Compiler) resolve(name string) (depth, slot int, ok bool) {
	for s := c.sc; s != nil; s, depth = s.outer, depth+1 {
		if slot = slices.Index(s.names, name); slot >= 0 {
			return depth, slot, true
		}
	}
	return 0, 0, false
}

// stmts compiles statements; with value set it leaves exactly one value (that
// of a final expression statement, else null) on the stack.
func (c *Compiler) stmts(list []ast.Stmt, value bool) {
	for i, s := range list {
		if es, ok := s.(*ast.ExprStmt); ok && value && i == len(list)-1 {
			c.expr(es.X)
			return
		}
		c.stmt(s)
	}
	if value {
		c.emit(code.OpNull, none)
	}
}

func (c *Compiler) scoped(b *ast.Block, value bool) {
	if len(b.Lets) == 0 {
		c.stmts(b.Stmts, value)
		return
	}
	c.sc = &scope{outer: c.sc}
	c.sc.declare(b.Lets...)
	c.emit(code.OpEnter, none, len(c.sc.names))
	c.stmts(b.Stmts, value)
	c.emit(code.OpLeave, none)
	c.sc = c.sc.outer
}

func (c *Compiler) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		c.expr(s.X)
		c.emit(code.OpPop, none)
	case *ast.Let:
		c.expr(s.Value)
		c.emit(code.OpSet, none, 0, slices.Index(c.sc.names, s.Name))
	case *ast.Assign:
		c.expr(s.Value)
		if depth, slot, ok := c.resolve(s.Target.Name); ok {
			c.emit(code.OpSet, s.Target.At, depth, slot)
		} else {
			c.emit(code.OpUndef, s.Target.At, c.konst(s.Target.Name))
		}
	case *ast.Return:
		c.expr(s.Value)
		c.emit(code.OpReturn, none)
	case *ast.While:
		top := len(c.fn.Code)
		c.expr(s.Cond)
		exit := c.emit(code.OpJumpFalse, s.At, 0)
		c.scoped(s.Body, false)
		c.emit(code.OpLoop, s.At, top)
		c.patch(exit)
	default:
		panic("compiler: unhandled statement")
	}
}

func (c *Compiler) expr(n ast.Expr) {
	switch n := n.(type) {
	case *ast.IntLit:
		c.emit(code.OpConst, none, c.konst(n.Val))
	case *ast.StrLit:
		c.emit(code.OpConst, none, c.konst(n.Val))
	case *ast.BoolLit:
		op := code.OpFalse
		if n.Val {
			op = code.OpTrue
		}
		c.emit(op, none)
	case *ast.NullLit:
		c.emit(code.OpNull, none)
	case *ast.Ident:
		if depth, slot, ok := c.resolve(n.Name); ok {
			c.emit(code.OpGet, n.At, depth, slot)
		} else if b := object.Builtins[n.Name]; b != nil {
			c.emit(code.OpConst, n.At, c.konst(b))
		} else {
			c.emit(code.OpUndef, n.At, c.konst(n.Name))
		}
	case *ast.Array:
		c.exprs(n.Elems)
		c.emit(code.OpArray, none, len(n.Elems))
	case *ast.Infix:
		c.expr(n.L)
		c.expr(n.R)
		c.emit(code.OpBin, n.At, int(n.Op))
	case *ast.If:
		c.expr(n.Cond)
		els := c.emit(code.OpJumpFalse, none, 0)
		c.scoped(n.Then, true)
		end := c.emit(code.OpJump, none, 0)
		c.patch(els)
		if n.Else != nil {
			c.scoped(n.Else, true)
		} else {
			c.emit(code.OpNull, none)
		}
		c.patch(end)
	case *ast.Fn:
		c.function(n)
	case *ast.Call:
		c.expr(n.Fn)
		c.exprs(n.Args)
		c.emit(code.OpCall, n.At, len(n.Args))
	case *ast.Index:
		c.expr(n.X)
		c.expr(n.I)
		c.emit(code.OpIndex, n.At)
	default:
		panic("compiler: unhandled expression")
	}
}

func (c *Compiler) exprs(xs []ast.Expr) {
	for _, x := range xs {
		c.expr(x)
	}
}

func (c *Compiler) function(n *ast.Fn) {
	outerFn, outerSc := c.fn, c.sc
	c.fn, c.sc = &code.Fn{NParams: len(n.Params)}, &scope{outer: outerSc}
	c.sc.declare(n.Params...)
	c.sc.declare(n.Body.Lets...)
	c.stmts(n.Body.Stmts, true)
	c.emit(code.OpReturn, none)
	c.fn.Slots = len(c.sc.names)
	fn := c.fn
	c.fn, c.sc = outerFn, outerSc
	c.emit(code.OpClosure, none, c.konst(fn))
}
