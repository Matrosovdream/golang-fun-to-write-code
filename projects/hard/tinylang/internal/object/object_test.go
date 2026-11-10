package object

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang/internal/token"
)

func TestInfix(t *testing.T) {
	arr := &Array{}
	tests := []struct {
		op      token.Kind
		l, r    any
		want    any
		wantErr string
	}{
		{token.PLUS, int64(2), int64(3), int64(5), ""},
		{token.MINUS, int64(2), int64(3), int64(-1), ""},
		{token.STAR, int64(4), int64(3), int64(12), ""},
		{token.SLASH, int64(-7), int64(2), int64(-3), ""},
		{token.PERCENT, int64(-7), int64(3), int64(-1), ""},
		{token.SLASH, int64(math.MinInt64), int64(-1), int64(math.MinInt64), ""}, // wraps, does not panic
		{token.SLASH, int64(1), int64(0), nil, "division by zero"},
		{token.PERCENT, int64(1), int64(0), nil, "division by zero"},
		{token.LT, int64(1), int64(2), true, ""},
		{token.GE, int64(1), int64(2), false, ""},
		{token.PLUS, "a", "b", "ab", ""},
		{token.LE, "a", "a", true, ""},
		{token.GT, "b", "a", true, ""},
		{token.EQ, arr, arr, true, ""},
		{token.EQ, arr, &Array{}, false, ""},
		{token.EQ, nil, nil, true, ""},
		{token.NE, int64(1), "1", true, ""},
		{token.PLUS, int64(1), true, nil, "type mismatch: int + bool"},
		{token.PLUS, "a", int64(1), nil, "type mismatch: string + int"},
		{token.MINUS, "a", "b", nil, "type mismatch: string - string"},
		{token.LT, int64(1), "a", nil, "type mismatch: int < string"},
		{token.STAR, nil, nil, nil, "type mismatch: null * null"},
		{token.LT, arr, arr, nil, "type mismatch: array < array"},
	}
	for _, tt := range tests {
		got, err := Infix(token.Pos{Line: 1, Col: 1}, tt.op, tt.l, tt.r)
		if tt.wantErr != "" {
			require.ErrorContains(t, err, tt.wantErr)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestIndex(t *testing.T) {
	arr := &Array{Elems: []any{int64(10), "x"}}
	for _, tt := range []struct {
		x, i    any
		want    any
		wantErr string
	}{
		{arr, int64(1), "x", ""},
		{"héllo", int64(1), "\xc3", ""}, // strings index by byte
		{arr, int64(2), nil, "index out of range: 2 (length 2)"},
		{arr, int64(-1), nil, "index out of range: -1 (length 2)"},
		{"ab", int64(2), nil, "index out of range: 2 (length 2)"},
		{arr, "0", nil, "index must be int, got string"},
		{int64(5), int64(0), nil, "cannot index int"},
	} {
		got, err := Index(token.Pos{}, tt.x, tt.i)
		if tt.wantErr != "" {
			require.ErrorContains(t, err, tt.wantErr)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tt.want, got)
	}
}

func TestInspect(t *testing.T) {
	cyc := &Array{Elems: []any{int64(1)}}
	cyc.Elems = append(cyc.Elems, cyc)
	shared := &Array{}
	for _, tt := range []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{int64(-3), "-3"},
		{true, "true"},
		{"a\"b\n", `"a\"b\n"`},
		{&Array{Elems: []any{int64(1), "s", nil, &Array{}}}, `[1, "s", null, []]`},
		{cyc, "[1, [...]]"},
		{&Array{Elems: []any{shared, shared}}, "[[], []]"}, // sharing is not a cycle
		{Builtins["len"], "<builtin len>"},
		{struct{}{}, "<fn>"},
	} {
		require.Equal(t, tt.want, Inspect(tt.v))
	}
	require.Equal(t, "raw", Display("raw"))
	require.Equal(t, `["raw"]`, Display(&Array{Elems: []any{"raw"}}))
}

func TestMeter(t *testing.T) {
	m := Meter{MaxSteps: 3}
	pos := token.Pos{Line: 2, Col: 4}
	require.ErrorContains(t, m.Enter(pos, 2, 1), "wrong number of arguments: want 2, got 1")
	for range 3 {
		require.NoError(t, m.Tick(pos))
	}
	require.ErrorContains(t, m.Tick(pos), "2:4: runtime error: step limit exceeded")
	m.Reset()
	require.NoError(t, m.Tick(pos))

	m = Meter{}
	for range MaxDepth {
		require.NoError(t, m.Enter(pos, 0, 0))
	}
	require.ErrorContains(t, m.Enter(pos, 0, 0), "stack overflow")
	m.Leave()
	require.NoError(t, m.Enter(pos, 0, 0), "leaving a call frees a slot")
}

func TestBuiltins(t *testing.T) {
	call := func(name string, args ...any) (any, error) {
		return CallBuiltin(token.Pos{Line: 1, Col: 1}, Builtins[name], nil, args)
	}
	arr := &Array{}
	got, err := call("push", arr, int64(1))
	require.NoError(t, err)
	require.Same(t, arr, got, "push mutates in place and returns the array")
	require.Equal(t, []any{int64(1)}, arr.Elems)

	got, err = call("len", "héllo")
	require.NoError(t, err)
	require.Equal(t, int64(6), got)

	_, err = call("push", "no", int64(1))
	require.ErrorContains(t, err, "push: first argument must be an array, got string")
	_, err = call("len")
	require.ErrorContains(t, err, "wrong number of arguments: want 1, got 0")
}
