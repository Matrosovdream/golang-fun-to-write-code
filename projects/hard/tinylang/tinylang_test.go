package tinylang_test

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang"
	"tinylang/internal/object"
	"tinylang/internal/parser"
)

var update = flag.Bool("update", false, "rewrite testdata/*.out from the eval engine (then read the diff!)")

// A runaway program must fail fast on both engines, even under -race.
const stepLimit = 500_000

var engines = []string{"eval", "vm"}

// runProgram returns what the program printed, plus "error: ..." if it failed.
func runProgram(t *testing.T, engine, src string) string {
	t.Helper()
	var out strings.Builder
	e, err := tinylang.NewEngine(engine, &out, stepLimit)
	require.NoError(t, err)
	if _, err := tinylang.Run(e, src); err != nil {
		fmt.Fprintf(&out, "error: %v\n", err)
	}
	return out.String()
}

// Differential test: every program in testdata/ must produce exactly the
// golden output on both engines — printed text, error message and position.
func TestDifferential(t *testing.T) {
	files, err := filepath.Glob("testdata/*.tl")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		golden := strings.TrimSuffix(file, ".tl") + ".out"
		t.Run(strings.TrimSuffix(filepath.Base(file), ".tl"), func(t *testing.T) {
			src, err := os.ReadFile(file)
			require.NoError(t, err)
			if *update {
				require.NoError(t, os.WriteFile(golden, []byte(runProgram(t, "eval", string(src))), 0o644))
			}
			want, err := os.ReadFile(golden)
			require.NoError(t, err)
			for _, engine := range engines {
				t.Run(engine, func(t *testing.T) {
					require.Equal(t, string(want), runProgram(t, engine, string(src)))
				})
			}
		})
	}
}

func TestEngines(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{"arithmetic", "2 + 3 * 4 - 10 / 5 % 3", "12"},
		{"negative division truncates", "[-7 / 2, -7 % 3]", "[-3, -1]"},
		{"int wraps", "9223372036854775807 + 1", "-9223372036854775808"},
		{"strings", `"ab" + "cd" < "abce"`, "true"},
		{"equality", `[1 == 1, "a" == "a", null == null, 1 == true, [] == []]`, "[true, true, true, false, false]"},
		{"&& and || are sugar for if", `[1 && 2, null && 2, false || "x", 0 || 1]`, `[2, false, "x", true]`},
		{"if is an expression", `if 1 < 2 { "yes" } else { "no" }`, `"yes"`},
		{"if without else is null", `if false { 1 }`, "null"},
		{"while is null", "while false { }", "null"},
		{"block value is last expression", "let f = fn() { 1; 2 }; f()", "2"},
		{"block ending in let is null", "let f = fn() { let x = 1 }; f()", "null"},
		{"empty fn", "fn() {}()", "null"},
		{"closure counter", "let mk = fn() { let n = 0; fn() { n = n + 1 } }; let c = mk(); c(); c(); let d = mk(); d()", "null"},
		{"counter reads back", "let mk = fn() { let n = 0; [fn() { n = n + 1 }, fn() { n }] }; let p = mk(); p[0](); p[0](); p[1]()", "2"},
		{"recursion", "let f = fn(n) { if n < 2 { n } else { f(n - 1) + f(n - 2) } }; f(20)", "6765"},
		{"mutual recursion", "let e = fn(n) { if n == 0 { true } else { o(n - 1) } }; let o = fn(n) { if n == 0 { false } else { e(n - 1) } }; e(11)", "false"},
		{"fresh binding per iteration", "let fs = []; let i = 0; while i < 3 { let j = i; push(fs, fn() { j }); i = i + 1 } [fs[0](), fs[1](), fs[2]()]", "[0, 1, 2]"},
		{"shadow in block", "let x = 1; let y = if true { let x = 2; x } else { 0 }; [x, y]", "[1, 2]"},
		{"param shadows global", "let x = 1; let f = fn(x) { x }; [f(5), x]", "[5, 1]"},
		{"let reuses binding in same scope", "let x = 1; let f = fn() { x }; let x = 2; f()", "2"},
		{"top-level return", "return 5; 6", "5"},
		{"return inside expression", "let f = fn(c) { 10 + if c { return 1 } else { 2 } }; [f(true), f(false)]", "[1, 12]"},
		{"builtins", `[len("héllo"), len([1, 2]), str(12), str("a"), push([], 1)]`, `[6, 2, "12", "a", [1]]`},
		{"builtin as value", "let f = len; f([1])", "1"},
		{"cyclic array prints", "let a = [1]; push(a, a); a", "[1, [...]]"},
		{"string index is a byte", `"abc"[1]`, `"b"`},

		{"type mismatch", "1 + true", "error: 1:3: runtime error: type mismatch: int + bool"},
		{"string minus", `"a" - "b"`, `error: 1:5: runtime error: type mismatch: string - string`},
		{"compare mixed", `1 < "a"`, "error: 1:3: runtime error: type mismatch: int < string"},
		{"negate string", `-"a"`, "error: 1:1: runtime error: type mismatch: int - string"},
		{"division by zero", "5 / (2 - 2)", "error: 1:3: runtime error: division by zero"},
		{"modulo by zero", "5 % 0", "error: 1:3: runtime error: division by zero"},
		{"index out of range", "[1, 2][2]", "error: 1:7: runtime error: index out of range: 2 (length 2)"},
		{"index not int", `[1]["a"]`, "error: 1:4: runtime error: index must be int, got string"},
		{"index a number", "5[0]", "error: 1:2: runtime error: cannot index int"},
		{"call non-function", "let x = 1; x()", "error: 1:13: runtime error: cannot call int"},
		{"call null", "null(1)", "error: 1:5: runtime error: cannot call null"},
		{"too few arguments", "fn(a, b) {}(1)", "error: 1:12: runtime error: wrong number of arguments: want 2, got 1"},
		{"too many arguments", "fn() {}(1)", "error: 1:8: runtime error: wrong number of arguments: want 0, got 1"},
		{"builtin arity", "len(1, 2)", "error: 1:4: runtime error: wrong number of arguments: want 1, got 2"},
		{"builtin type", "len(5)", "error: 1:4: runtime error: len: unsupported argument int"},
		{"undefined", "x", `error: 1:1: runtime error: undefined variable "x"`},
		{"assign undefined", "x = 1", `error: 1:1: runtime error: undefined variable "x"`},
		{"used before definition", "let a = b; let b = 1", "error: 1:9: runtime error: variable used before its definition"},
		{"shadow is not visible early", "let x = 1; if true { let y = x; let x = 2 }", "error: 1:30: runtime error: variable used before its definition"},
		{"stack overflow", "let f = fn() { f() }; f()", "error: 1:17: runtime error: stack overflow"},
		{"step limit", "while true { }", "error: 1:1: runtime error: step limit exceeded"},
		{"illegal character", "let f = fn(x) { x.y }; f(1)", "error: 1:18: syntax error: illegal character '.'"},
		{"syntax error", "let = 1", `error: 1:5: syntax error: expected "identifier", found "="`},
	}
	for _, tt := range tests {
		for _, engine := range engines {
			t.Run(tt.name+"/"+engine, func(t *testing.T) {
				e, err := tinylang.NewEngine(engine, io.Discard, stepLimit)
				require.NoError(t, err)
				v, err := tinylang.Run(e, tt.src)
				if err != nil {
					require.Equal(t, tt.want, "error: "+err.Error())
					return
				}
				require.Equal(t, tt.want, object.Inspect(v))
			})
		}
	}
}

// A REPL keeps one engine alive: globals persist, and an error does not break it.
func TestSessions(t *testing.T) {
	for _, engine := range engines {
		t.Run(engine, func(t *testing.T) {
			e, err := tinylang.NewEngine(engine, io.Discard, stepLimit)
			require.NoError(t, err)
			step := func(src string) string {
				v, err := tinylang.Run(e, src)
				if err != nil {
					return "error: " + err.Error()
				}
				return object.Inspect(v)
			}
			require.Equal(t, "null", step("let x = 1"))
			require.Equal(t, "2", step("let bump = fn() { x = x + 1; x }; bump()"))
			require.Equal(t, "3", step("bump()"))
			require.Equal(t, "error: 1:3: runtime error: type mismatch: int + string", step(`x + "a"`))
			require.Equal(t, "error: 1:17: runtime error: stack overflow", step("let f = fn() { f() }; f()"))
			require.Equal(t, "4", step("bump()"))
			require.Equal(t, "4", step("let x = x; x"), "a redefinition sees the old value")
			require.Equal(t, "12", step("let y = 10; y + 2"))
			require.Equal(t, `error: 1:5: syntax error: unexpected ")"`, step("1 + )"))
			require.Equal(t, "12", step("y + 2"))
		})
	}
}

// cappedWriter stops a fuzzed program from printing gigabytes.
type cappedWriter struct{ strings.Builder }

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.Len() > 1<<20 {
		return 0, fmt.Errorf("output limit")
	}
	return w.Builder.Write(p)
}

// Differential fuzzing: any input that parses must behave identically on both
// engines — same output, same final value, same error text and position.
func FuzzEngines(f *testing.F) {
	files, err := filepath.Glob("testdata/*.tl")
	require.NoError(f, err)
	for _, file := range files {
		src, err := os.ReadFile(file)
		require.NoError(f, err)
		f.Add(string(src))
	}
	f.Fuzz(func(t *testing.T, src string) {
		prog, err := parser.Parse(src)
		if err != nil {
			return
		}
		var got [2]string
		for i, engine := range engines {
			var out cappedWriter
			e, err := tinylang.NewEngine(engine, &out, 5000)
			require.NoError(t, err)
			v, err := e(prog)
			got[i] = fmt.Sprintf("%s\n=> %s, %v", out.String(), object.Inspect(v), err)
		}
		require.Equal(t, got[0], got[1], src)
	})
}
