package compiler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang/internal/code"
	"tinylang/internal/parser"
	"tinylang/internal/token"
)

func ins(op code.Op, a, b int) code.Instr { return code.Instr{Op: op, A: int32(a), B: int32(b)} }

func compile(t *testing.T, src string) *code.Fn {
	t.Helper()
	prog, err := parser.Parse(src)
	require.NoError(t, err)
	return New().Compile(prog)
}

func TestCompile(t *testing.T) {
	plus, lt := int(token.PLUS), int(token.LT)
	tests := []struct {
		name, src string
		want      []code.Instr
	}{
		{"arithmetic keeps its value", "1 + 2", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpConst, 1, 0), ins(code.OpBin, plus, 0), ins(code.OpReturn, 0, 0)}},
		{"statement value is popped", "1; 2", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpPop, 0, 0), ins(code.OpConst, 1, 0), ins(code.OpReturn, 0, 0)}},
		{"let then read", "let x = 1; x", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpSet, 0, 0), ins(code.OpGet, 0, 0), ins(code.OpReturn, 0, 0)}},
		{"while loops back to its condition", "let i = 0; while i < 3 { i = i + 1 }", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpSet, 0, 0), // let i = 0
			ins(code.OpGet, 0, 0), ins(code.OpConst, 1, 0), ins(code.OpBin, lt, 0), ins(code.OpJumpFalse, 11, 0),
			ins(code.OpGet, 0, 0), ins(code.OpConst, 2, 0), ins(code.OpBin, plus, 0), ins(code.OpSet, 0, 0),
			ins(code.OpLoop, 2, 0),
			ins(code.OpNull, 0, 0), ins(code.OpReturn, 0, 0)}},
		{"a block with let opens a scope", "if true { let x = 1; x }", []code.Instr{
			ins(code.OpTrue, 0, 0), ins(code.OpJumpFalse, 8, 0),
			ins(code.OpEnter, 1, 0), ins(code.OpConst, 0, 0), ins(code.OpSet, 0, 0), ins(code.OpGet, 0, 0), ins(code.OpLeave, 0, 0),
			ins(code.OpJump, 9, 0),
			ins(code.OpNull, 0, 0), ins(code.OpReturn, 0, 0)}},
		{"a block without let does not", "if true { 1 }", []code.Instr{
			ins(code.OpTrue, 0, 0), ins(code.OpJumpFalse, 4, 0), ins(code.OpConst, 0, 0), ins(code.OpJump, 5, 0),
			ins(code.OpNull, 0, 0), ins(code.OpReturn, 0, 0)}},
		{"builtin is a constant, unknown name fails at run time", "[len, nope]", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpUndef, 1, 0), ins(code.OpArray, 2, 0), ins(code.OpReturn, 0, 0)}},
		{"call", "len([])(1, 2)", []code.Instr{
			ins(code.OpConst, 0, 0), ins(code.OpArray, 0, 0), ins(code.OpCall, 1, 0),
			ins(code.OpConst, 1, 0), ins(code.OpConst, 2, 0), ins(code.OpCall, 2, 0), ins(code.OpReturn, 0, 0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, compile(t, tt.src).Code)
		})
	}
}

func TestFunctionsAndVariables(t *testing.T) {
	main := compile(t, "let f = fn(a) { let b = 1; fn() { a + b } }")
	require.Equal(t, 1, main.Slots)
	require.Equal(t, []code.Instr{ins(code.OpClosure, 0, 0), ins(code.OpSet, 0, 0), ins(code.OpNull, 0, 0), ins(code.OpReturn, 0, 0)}, main.Code)

	f := main.Consts[0].(*code.Fn)
	require.Equal(t, 1, f.NParams)
	require.Equal(t, 2, f.Slots, "parameters first, then the body's lets")

	inner := f.Consts[1].(*code.Fn)
	require.Equal(t, []code.Instr{
		ins(code.OpGet, 1, 0), ins(code.OpGet, 1, 1), ins(code.OpBin, int(token.PLUS), 0), ins(code.OpReturn, 0, 0),
	}, inner.Code, "a and b live one scope up")
}

func TestPositionsParallelCode(t *testing.T) {
	fn := compile(t, "let f = fn() { 1 +\n true }")
	for _, f := range []*code.Fn{fn, fn.Consts[0].(*code.Fn)} {
		require.Len(t, f.Pos, len(f.Code))
	}
	inner := fn.Consts[0].(*code.Fn)
	require.Equal(t, token.Pos{Line: 1, Col: 18}, inner.Pos[2], "the Bin instruction points at the +")
}

func TestREPLScopeGrows(t *testing.T) {
	c := New()
	for i, src := range []string{"let a = 1", "let b = a", "let a = 2"} {
		prog, err := parser.Parse(src)
		require.NoError(t, err)
		fn := c.Compile(prog)
		require.Equal(t, []int{1, 2, 2}[i], fn.Slots, src)
	}
}
