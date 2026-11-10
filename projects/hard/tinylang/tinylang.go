// Package tinylang wires the one shared parser to the two execution engines:
// a tree-walking evaluator and a bytecode compiler plus stack VM.
package tinylang

import (
	"embed"
	"fmt"
	"io"

	"tinylang/internal/ast"
	"tinylang/internal/compiler"
	"tinylang/internal/eval"
	"tinylang/internal/parser"
	"tinylang/internal/vm"
)

// Examples are the programs in testdata/, which double as the differential test corpus.
//
//go:embed testdata/*.tl
var Examples embed.FS

// Engine executes a parsed program against state that persists between calls.
type Engine func(*ast.Program) (any, error)

// NewEngine returns "eval" or "vm". maxSteps > 0 aborts runaway programs: it
// counts function calls plus loop iterations.
func NewEngine(name string, out io.Writer, maxSteps int) (Engine, error) {
	switch name {
	case "eval":
		return eval.New(out, maxSteps).Exec, nil
	case "vm":
		c, m := compiler.New(), vm.New(out, maxSteps)
		return func(p *ast.Program) (any, error) { return m.Run(c.Compile(p)) }, nil
	}
	return nil, fmt.Errorf("unknown engine %q (want eval or vm)", name)
}

// Run parses src and executes it, returning the value of its last expression.
func Run(e Engine, src string) (any, error) {
	prog, err := parser.Parse(src)
	if err != nil {
		return nil, err
	}
	return e(prog)
}
