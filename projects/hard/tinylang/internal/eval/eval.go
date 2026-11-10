// Package eval is the tree-walking engine: it executes the AST directly, with
// an environment chain (a small slice of bindings per scope) for variables and closures.
//
// Scoping rule shared with the compiler: a block gets a scope only if it has
// `let`s, and all of them are declared (as Unset) when the block is entered, so
// functions can refer to later definitions and reading one early is an error.
// `return` travels up the Go call stack as an error value (*returned).
package eval

import (
	"io"

	"tinylang/internal/ast"
	"tinylang/internal/object"
)

type binding struct {
	name string
	val  any
}

// env is one scope: a short slice scanned linearly, which beats a map at the
// handful of variables a scope really has.
type env struct {
	vars  []binding
	outer *env
}

func newEnv(outer *env, size int) *env { return &env{make([]binding, 0, size), outer} }

func (e *env) local(name string) *any {
	for i := range e.vars {
		if e.vars[i].name == name {
			return &e.vars[i].val
		}
	}
	return nil
}

// slot finds name in the nearest scope that declares it.
func (e *env) slot(name string) *any {
	for ; e != nil; e = e.outer {
		if p := e.local(name); p != nil {
			return p
		}
	}
	return nil
}

func (e *env) declare(names []string) {
	for _, n := range names {
		if e.local(n) == nil {
			e.vars = append(e.vars, binding{n, object.Unset})
		}
	}
}

type closure struct {
	fn  *ast.Fn
	env *env
}

type returned struct{ v any }

func (*returned) Error() string { return "return" }

// Interp keeps its global scope between Exec calls, which is what a REPL needs.
type Interp struct {
	object.Meter
	out    io.Writer
	global *env
}

func New(out io.Writer, maxSteps int) *Interp {
	return &Interp{Meter: object.Meter{MaxSteps: maxSteps}, out: out, global: newEnv(nil, 0)}
}

func (in *Interp) Exec(p *ast.Program) (any, error) {
	in.Reset()
	in.global.declare(p.Lets)
	v, err := in.block(&p.Block, in.global)
	if r, ok := err.(*returned); ok {
		return r.v, nil
	}
	return v, err
}

// block runs statements in e; its value is that of a final expression statement.
func (in *Interp) block(b *ast.Block, e *env) (v any, err error) {
	for _, s := range b.Stmts {
		if v, err = in.exec(s, e); err != nil {
			return nil, err
		}
	}
	return v, nil
}

func (in *Interp) scoped(b *ast.Block, outer *env) (any, error) {
	if len(b.Lets) > 0 {
		outer = newEnv(outer, len(b.Lets))
		outer.declare(b.Lets)
	}
	return in.block(b, outer)
}

func (in *Interp) exec(s ast.Stmt, e *env) (any, error) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return in.eval(s.X, e)
	case *ast.Let:
		v, err := in.eval(s.Value, e)
		if err != nil {
			return nil, err
		}
		*e.slot(s.Name) = v
	case *ast.Assign:
		return nil, in.assign(s, e)
	case *ast.Return:
		v, err := in.eval(s.Value, e)
		if err != nil {
			return nil, err
		}
		return nil, &returned{v}
	case *ast.While:
		for {
			c, err := in.eval(s.Cond, e)
			if err != nil || !object.Truthy(c) {
				return nil, err
			}
			if _, err := in.scoped(s.Body, e); err != nil {
				return nil, err
			}
			if err := in.Tick(s.At); err != nil {
				return nil, err
			}
		}
	default:
		panic("eval: unhandled statement")
	}
	return nil, nil
}

func (in *Interp) assign(s *ast.Assign, e *env) error {
	v, err := in.eval(s.Value, e)
	if err != nil {
		return err
	}
	p := e.slot(s.Target.Name)
	if p == nil {
		return object.Errorf(s.Target.At, "undefined variable %q", s.Target.Name)
	}
	*p = v
	return nil
}

func (in *Interp) evalList(xs []ast.Expr, e *env) ([]any, error) {
	out := make([]any, len(xs))
	for i, x := range xs {
		var err error
		if out[i], err = in.eval(x, e); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (in *Interp) eval(n ast.Expr, e *env) (any, error) {
	switch n := n.(type) {
	case *ast.IntLit:
		return n.Val, nil
	case *ast.StrLit:
		return n.Val, nil
	case *ast.BoolLit:
		return n.Val, nil
	case *ast.NullLit:
		return nil, nil
	case *ast.Ident:
		if p := e.slot(n.Name); p != nil {
			if *p == object.Unset {
				return nil, object.Errorf(n.At, "variable used before its definition")
			}
			return *p, nil
		}
		if b := object.Builtins[n.Name]; b != nil {
			return b, nil
		}
		return nil, object.Errorf(n.At, "undefined variable %q", n.Name)
	case *ast.Array:
		elems, err := in.evalList(n.Elems, e)
		return &object.Array{Elems: elems}, err
	case *ast.Infix:
		l, err := in.eval(n.L, e)
		if err != nil {
			return nil, err
		}
		r, err := in.eval(n.R, e)
		if err != nil {
			return nil, err
		}
		return object.Infix(n.At, n.Op, l, r)
	case *ast.If:
		c, err := in.eval(n.Cond, e)
		switch {
		case err != nil:
			return nil, err
		case object.Truthy(c):
			return in.scoped(n.Then, e)
		case n.Else != nil:
			return in.scoped(n.Else, e)
		}
		return nil, nil
	case *ast.Fn:
		return &closure{n, e}, nil
	case *ast.Call:
		return in.call(n, e)
	case *ast.Index:
		x, err := in.eval(n.X, e)
		if err != nil {
			return nil, err
		}
		i, err := in.eval(n.I, e)
		if err != nil {
			return nil, err
		}
		return object.Index(n.At, x, i)
	}
	panic("eval: unhandled expression")
}

func (in *Interp) call(n *ast.Call, e *env) (any, error) {
	f, err := in.eval(n.Fn, e)
	if err != nil {
		return nil, err
	}
	args, err := in.evalList(n.Args, e)
	if err != nil {
		return nil, err
	}
	switch f := f.(type) {
	case *object.Builtin:
		return object.CallBuiltin(n.At, f, in.out, args)
	case *closure:
		if err := in.Enter(n.At, len(f.fn.Params), len(args)); err != nil {
			return nil, err
		}
		scope := newEnv(f.env, len(args)+len(f.fn.Body.Lets))
		for i, p := range f.fn.Params {
			scope.vars = append(scope.vars, binding{p, args[i]})
		}
		scope.declare(f.fn.Body.Lets)
		v, err := in.block(f.fn.Body, scope)
		in.Leave()
		if r, ok := err.(*returned); ok {
			return r.v, nil
		}
		return v, err
	}
	return nil, object.Errorf(n.At, "cannot call %s", object.TypeName(f))
}
