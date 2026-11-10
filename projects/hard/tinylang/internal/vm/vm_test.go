package vm

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang/internal/code"
	"tinylang/internal/object"
	"tinylang/internal/token"
)

// The VM runs hand-assembled bytecode without the compiler in the loop.
func TestRunHandAssembled(t *testing.T) {
	at := func(col int) token.Pos { return token.Pos{Line: 1, Col: col} }
	prog := func(consts ...any) *code.Fn {
		return &code.Fn{
			Consts: consts,
			Code: []code.Instr{
				{Op: code.OpConst, A: 0}, {Op: code.OpConst, A: 1},
				{Op: code.OpBin, A: int32(token.SLASH)}, {Op: code.OpReturn},
			},
			Pos: []token.Pos{at(1), at(5), at(3), at(0)},
		}
	}
	m := New(io.Discard, 0)
	v, err := m.Run(prog(int64(84), int64(2)))
	require.NoError(t, err)
	require.Equal(t, int64(42), v)

	_, err = m.Run(prog(int64(1), int64(0)))
	var rt *object.RuntimeError
	require.ErrorAs(t, err, &rt)
	require.Equal(t, at(3), rt.Pos, "the error carries the position of the failing instruction")
	require.Equal(t, "division by zero", rt.Msg)

	v, err = m.Run(prog(int64(9), int64(3)))
	require.NoError(t, err, "a failed run leaves the VM reusable")
	require.Equal(t, int64(3), v)
}

func TestCallingConvention(t *testing.T) {
	// fn(a, b) { b }: callee and arguments sit on the stack below the frame's base.
	double := &code.Fn{NParams: 2, Slots: 2, Code: []code.Instr{{Op: code.OpGet, A: 0, B: 1}, {Op: code.OpReturn}}, Pos: make([]token.Pos, 2)}
	main := &code.Fn{
		Consts: []any{double, int64(1), int64(2)},
		Code: []code.Instr{
			{Op: code.OpClosure, A: 0}, {Op: code.OpConst, A: 1}, {Op: code.OpConst, A: 2}, {Op: code.OpCall, A: 2}, {Op: code.OpReturn},
		},
		Pos: make([]token.Pos, 5),
	}
	m := New(io.Discard, 0)
	v, err := m.Run(main)
	require.NoError(t, err)
	require.Equal(t, int64(2), v)
	require.Empty(t, m.stack, "return unwinds the callee, its arguments and temporaries")

	main.Code = []code.Instr{{Op: code.OpClosure, A: 0}, {Op: code.OpConst, A: 1}, {Op: code.OpCall, A: 1}, {Op: code.OpReturn}}
	_, err = m.Run(main)
	require.ErrorContains(t, err, "wrong number of arguments: want 2, got 1")
}
