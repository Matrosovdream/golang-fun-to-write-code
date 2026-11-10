// Package vm executes compiler output on a stack machine: an operand stack, a
// stack of call frames, and an environment chain for variables.
//
// A closure is code plus the environment it was created in. That is simpler
// than flat closures with copied free variables (see the README exercises) and
// it gets mutation of captured variables right for free, because every closure
// shares the very same environment object.
package vm

import (
	"io"
	"slices"

	"tinylang/internal/code"
	"tinylang/internal/object"
	"tinylang/internal/token"
)

type env struct {
	vars   []any
	parent *env
}

func newEnv(parent *env, slots int) *env {
	e := &env{make([]any, slots), parent}
	for i := range e.vars {
		e.vars[i] = object.Unset
	}
	return e
}

type closure struct {
	fn  *code.Fn
	env *env
}

type frame struct {
	fn   *code.Fn
	ip   int
	base int  // stack index of the callee: everything above is this call's
	env  *env // the caller's environment, restored on return
}

// pos is where the instruction being executed came from; only errors need it.
func (f *frame) pos() token.Pos { return f.fn.Pos[f.ip-1] }

// VM keeps its global environment between Run calls, which is what a REPL needs.
type VM struct {
	object.Meter
	out     io.Writer
	stack   []any
	frames  []frame
	env     *env
	globals *env
}

func New(out io.Writer, maxSteps int) *VM {
	return &VM{Meter: object.Meter{MaxSteps: maxSteps}, out: out, globals: &env{}, frames: make([]frame, 0, 64)}
}

func (m *VM) push(v any) { m.stack = append(m.stack, v) }

func (m *VM) pop() any {
	v := m.stack[len(m.stack)-1]
	m.stack = m.stack[:len(m.stack)-1]
	return v
}

func (m *VM) scope(up int32) *env {
	e := m.env
	for ; up > 0; up-- {
		e = e.parent
	}
	return e
}

func (m *VM) Run(main *code.Fn) (any, error) {
	for len(m.globals.vars) < main.Slots {
		m.globals.vars = append(m.globals.vars, object.Unset)
	}
	m.Reset()
	m.stack, m.env = m.stack[:0], m.globals
	m.frames = append(m.frames[:0], frame{fn: main})
	for {
		f := &m.frames[len(m.frames)-1]
		in := f.fn.Code[f.ip]
		f.ip++
		switch in.Op {
		case code.OpConst:
			m.push(f.fn.Consts[in.A])
		case code.OpNull:
			m.push(nil)
		case code.OpTrue:
			m.push(true)
		case code.OpFalse:
			m.push(false)
		case code.OpPop:
			m.pop()
		case code.OpGet:
			v := m.scope(in.A).vars[in.B]
			if v == object.Unset {
				return nil, object.Errorf(f.pos(), "variable used before its definition")
			}
			m.push(v)
		case code.OpSet:
			m.scope(in.A).vars[in.B] = m.pop()
		case code.OpUndef:
			return nil, object.Errorf(f.pos(), "undefined variable %q", f.fn.Consts[in.A])
		case code.OpBin:
			r, l := m.pop(), m.pop()
			if a, ok := l.(int64); ok { // fast path: skip Infix's generic dispatch
				if b, ok := r.(int64); ok {
					switch token.Kind(in.A) {
					case token.PLUS:
						m.push(a + b)
						continue
					case token.MINUS:
						m.push(a - b)
						continue
					case token.LT:
						m.push(a < b)
						continue
					}
				}
			}
			v, err := object.Infix(f.pos(), token.Kind(in.A), l, r)
			if err != nil {
				return nil, err
			}
			m.push(v)
		case code.OpJump:
			f.ip = int(in.A)
		case code.OpJumpFalse:
			if !object.Truthy(m.pop()) {
				f.ip = int(in.A)
			}
		case code.OpLoop:
			if err := m.Tick(f.pos()); err != nil {
				return nil, err
			}
			f.ip = int(in.A)
		case code.OpCall:
			if err := m.call(int(in.A), f.pos()); err != nil {
				return nil, err
			}
		case code.OpReturn:
			v := m.pop()
			m.stack = m.stack[:f.base]
			if len(m.frames) == 1 {
				return v, nil
			}
			m.Leave()
			m.env = f.env
			m.frames = m.frames[:len(m.frames)-1]
			m.push(v)
		case code.OpClosure:
			m.push(&closure{f.fn.Consts[in.A].(*code.Fn), m.env})
		case code.OpArray:
			n := len(m.stack) - int(in.A)
			elems := slices.Clone(m.stack[n:])
			m.stack = append(m.stack[:n], &object.Array{Elems: elems})
		case code.OpIndex:
			i, x := m.pop(), m.pop()
			v, err := object.Index(f.pos(), x, i)
			if err != nil {
				return nil, err
			}
			m.push(v)
		case code.OpEnter:
			m.env = newEnv(m.env, int(in.A))
		case code.OpLeave:
			m.env = m.env.parent
		}
	}
}

// call invokes the callee sitting below its n arguments on the stack.
func (m *VM) call(n int, pos token.Pos) error {
	base := len(m.stack) - n - 1
	args := m.stack[base+1:]
	switch f := m.stack[base].(type) {
	case *object.Builtin:
		v, err := object.CallBuiltin(pos, f, m.out, args)
		if err != nil {
			return err
		}
		m.stack = append(m.stack[:base], v)
	case *closure:
		if err := m.Enter(pos, f.fn.NParams, n); err != nil {
			return err
		}
		scope := newEnv(f.env, f.fn.Slots)
		copy(scope.vars, args)
		m.frames = append(m.frames, frame{fn: f.fn, base: base, env: m.env})
		m.env = scope
	default:
		return object.Errorf(pos, "cannot call %s", object.TypeName(f))
	}
	return nil
}
