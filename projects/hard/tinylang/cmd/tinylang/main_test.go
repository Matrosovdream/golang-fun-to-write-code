package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBalanced(t *testing.T) {
	tests := map[string]bool{
		"1 + 2":                  true,
		"let f = fn() {":         false,
		"let f = fn() {\n  1\n}": true,
		"print(1,":               false,
		"[1, [2,":                false,
		`"a { string"`:           true,
		"// } comment ({":        true,
		"}":                      true,
		`"unterminated`:          true,
	}
	for src, want := range tests {
		require.Equal(t, want, balanced(src), src)
	}
}

func TestREPL(t *testing.T) {
	input := `let x = 5
x * 2
let add = fn(a, b) {
  a + b
}
add(x, 1)
"hi" + 1
[1, "two", [3]]
add(
  1,
  2)
let y = )
add(x, y)
`
	for _, engine := range []string{"eval", "vm"} {
		t.Run(engine, func(t *testing.T) {
			var out strings.Builder
			require.NoError(t, run(strings.NewReader(input), &out, engine, 0, "", nil))
			require.Equal(t, strings.Join([]string{
				">> >> 10",
				">> .. .. >> 6",
				`>> 1:6: runtime error: type mismatch: string + int`,
				`>> [1, "two", [3]]`,
				">> .. .. 3",
				`>> 1:9: syntax error: unexpected ")"`,
				`>> 1:8: runtime error: undefined variable "y"`,
				">> ",
			}, "\n"), out.String())
		})
	}
}

func TestRun(t *testing.T) {
	var out strings.Builder
	require.NoError(t, run(nil, &out, "vm", 0, "fib", nil))
	require.Contains(t, out.String(), "fib 15 = 610\n")

	require.ErrorContains(t, run(nil, &out, "jit", 0, "", nil), `unknown engine "jit"`)
	require.ErrorContains(t, run(nil, &out, "vm", 0, "nope", nil), "nope.tl")
	require.ErrorContains(t, run(nil, &out, "vm", 0, "", []string{"a", "b"}), "usage")
	require.ErrorContains(t, run(nil, &out, "eval", 1000, "err_steps", nil), "step limit exceeded")
}
