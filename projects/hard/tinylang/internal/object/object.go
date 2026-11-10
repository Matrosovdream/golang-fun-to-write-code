// Package object is what both engines share: value representation, operator
// semantics, indexing, builtins and runtime errors. Sharing it means the
// differential tests probe what really differs between the engines (scoping,
// calls, control flow), not two copies of integer division.
//
// A value is a plain Go value in an `any`: int64, bool, string, nil (null),
// *Array, *Builtin, or an engine's own closure type (printed as "<fn>").
package object

import (
	"cmp"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"

	"tinylang/internal/token"
)

const (
	MaxDepth  = 10_000 // active user-function calls before "stack overflow"
	maxString = 1 << 24
)

// Meter enforces the limits both engines must apply identically: call depth
// and an optional step budget (function calls + loop iterations).
type Meter struct {
	MaxSteps     int // 0 means unlimited
	steps, depth int
}

func (m *Meter) Reset() { m.steps, m.depth = 0, 0 }

func (m *Meter) Tick(pos token.Pos) error {
	if m.steps++; m.MaxSteps > 0 && m.steps > m.MaxSteps {
		return Errorf(pos, "step limit exceeded")
	}
	return nil
}

// Enter is called before running a closure: it checks arity, depth and budget.
func (m *Meter) Enter(pos token.Pos, want, got int) error {
	if want != got {
		return arity(pos, want, got)
	}
	if m.depth >= MaxDepth {
		return Errorf(pos, "stack overflow")
	}
	m.depth++
	return m.Tick(pos)
}

func (m *Meter) Leave() { m.depth-- }

func arity(pos token.Pos, want, got int) error {
	return Errorf(pos, "wrong number of arguments: want %d, got %d", want, got)
}

// Unset marks a hoisted `let` whose statement has not run yet.
var Unset = &struct{ _ byte }{}

type Array struct{ Elems []any }

type Builtin struct {
	Name  string
	Arity int // -1: any number
	Fn    func(out io.Writer, args []any) (any, error)
}

type RuntimeError struct {
	Pos token.Pos
	Msg string
}

func (e *RuntimeError) Error() string { return fmt.Sprintf("%v: runtime error: %s", e.Pos, e.Msg) }

func Errorf(pos token.Pos, format string, args ...any) error {
	return &RuntimeError{pos, fmt.Sprintf(format, args...)}
}

func Truthy(v any) bool { return v != nil && v != false }

func TypeName(v any) string {
	switch v.(type) {
	case nil:
		return "null"
	case int64:
		return "int"
	case bool:
		return "bool"
	case string:
		return "string"
	case *Array:
		return "array"
	}
	return "function"
}

func order[T cmp.Ordered](op token.Kind, a, b T) (any, bool) {
	switch op {
	case token.LT:
		return a < b, true
	case token.GT:
		return a > b, true
	case token.LE:
		return a <= b, true
	case token.GE:
		return a >= b, true
	}
	return nil, false
}

func Infix(pos token.Pos, op token.Kind, l, r any) (any, error) {
	switch op {
	case token.EQ:
		return l == r, nil // arrays and functions compare by identity
	case token.NE:
		return l != r, nil
	}
	switch a := l.(type) {
	case int64:
		if b, ok := r.(int64); ok {
			if v, ok := order(op, a, b); ok {
				return v, nil
			}
			switch op {
			case token.PLUS:
				return a + b, nil
			case token.MINUS:
				return a - b, nil
			case token.STAR:
				return a * b, nil
			case token.SLASH, token.PERCENT:
				if b == 0 {
					return nil, Errorf(pos, "division by zero")
				}
				if op == token.SLASH {
					return a / b, nil
				}
				return a % b, nil
			}
		}
	case string:
		if b, ok := r.(string); ok {
			if v, ok := order(op, a, b); ok {
				return v, nil
			}
			if op == token.PLUS {
				if len(a)+len(b) > maxString {
					return nil, Errorf(pos, "string too long")
				}
				return a + b, nil
			}
		}
	}
	return nil, Errorf(pos, "type mismatch: %s %v %s", TypeName(l), op, TypeName(r))
}

func Index(pos token.Pos, x, i any) (any, error) {
	n, ok := i.(int64)
	if !ok {
		return nil, Errorf(pos, "index must be int, got %s", TypeName(i))
	}
	switch x := x.(type) {
	case *Array:
		if n < 0 || n >= int64(len(x.Elems)) {
			return nil, Errorf(pos, "index out of range: %d (length %d)", n, len(x.Elems))
		}
		return x.Elems[n], nil
	case string:
		if n < 0 || n >= int64(len(x)) {
			return nil, Errorf(pos, "index out of range: %d (length %d)", n, len(x))
		}
		return x[n : n+1], nil
	}
	return nil, Errorf(pos, "cannot index %s", TypeName(x))
}

func CallBuiltin(pos token.Pos, b *Builtin, out io.Writer, args []any) (any, error) {
	if b.Arity >= 0 && len(args) != b.Arity {
		return nil, arity(pos, b.Arity, len(args))
	}
	v, err := b.Fn(out, args)
	if err != nil {
		return nil, Errorf(pos, "%s: %v", b.Name, err)
	}
	return v, nil
}

// Inspect renders a value as source-like text (strings quoted); Display is
// what print shows (strings raw).
func Inspect(v any) string { return inspect(v, nil) }

func Display(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return Inspect(v)
}

func inspect(v any, path []*Array) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(v)
	case *Array:
		if slices.Contains(path, v) { // cycle
			return "[...]"
		}
		parts := make([]string, len(v.Elems))
		for i, e := range v.Elems {
			parts[i] = inspect(e, append(path, v))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *Builtin:
		return "<builtin " + v.Name + ">"
	case int64, bool:
		return fmt.Sprint(v)
	}
	return "<fn>"
}

var Builtins = map[string]*Builtin{
	"len":   {Arity: 1, Fn: length},
	"push":  {Arity: 2, Fn: push},
	"print": {Arity: -1, Fn: printValues},
	"str":   {Arity: 1, Fn: func(_ io.Writer, a []any) (any, error) { return Display(a[0]), nil }},
}

func init() {
	for name, b := range Builtins {
		b.Name = name
	}
}

func length(_ io.Writer, a []any) (any, error) {
	switch x := a[0].(type) {
	case string:
		return int64(len(x)), nil
	case *Array:
		return int64(len(x.Elems)), nil
	}
	return nil, fmt.Errorf("unsupported argument %s", TypeName(a[0]))
}

// push mutates the array in place and returns it.
func push(_ io.Writer, a []any) (any, error) {
	arr, ok := a[0].(*Array)
	if !ok {
		return nil, fmt.Errorf("first argument must be an array, got %s", TypeName(a[0]))
	}
	arr.Elems = append(arr.Elems, a[1])
	return arr, nil
}

func printValues(out io.Writer, a []any) (any, error) {
	parts := make([]string, len(a))
	for i, v := range a {
		parts[i] = Display(v)
	}
	_, err := fmt.Fprintln(out, strings.Join(parts, " "))
	return nil, err
}
