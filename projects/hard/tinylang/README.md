# tinylang — a scripting language with two engines: tree-walker and bytecode VM

`tinylang prog.tl`, a REPL, and `-engine=eval|vm`: ints, bools, strings, arrays, `let`,
`if/else`, `while`, first-class functions and closures, builtins, errors reported as
`line:col`. One lexer and one parser feed two execution engines for the *same* language,
and a differential test proves they agree.

```
let makeCounter = fn() { let n = 0; fn() { n = n + 1; n } };
let c = makeCounter();
print(c(), c(), c());                       // 1 2 3
let map = fn(f, xs) { let out = []; let i = 0;
  while i < len(xs) { push(out, f(xs[i])); i = i + 1; } out };
print(map(fn(x) { x * x }, [1, 2, 3]));     // [1, 4, 9]
```

## What this project teaches

The pipeline: `source → lexer.Lex → []Token → parser.Parse → *ast.Program →` either
`eval.Interp.Exec` **or** `compiler.Compile → *code.Fn → vm.VM.Run`. `tinylang.go` is the thin
wiring (`NewEngine`, `Run`, the embedded examples); `internal/object` holds everything both
engines share (value representation, operator semantics, indexing, builtins, runtime errors, the
call-depth/step `Meter`), so the differential test probes what really differs between engines —
scoping, calls, control flow — not two copies of integer division.

- **Lexer** (`internal/lexer`): a hand-written scanner that cannot fail — bad input becomes
  `ILLEGAL` tokens carrying a message, and every call consumes a byte, so the parser can never
  be starved. Columns count characters, not bytes. Strings reuse `strconv.Unquote`, so
  `strconv.Quote` (the printer) round-trips with it exactly. `token.go` keeps kind names in one
  `strings.Fields` list.
- **AST as a sum type** (`internal/ast`): `Expr` and `Stmt` are interfaces with unexported
  marker methods; both engines switch over the concrete types and `panic` on an unknown one.
  `String()` prints valid source with every operator parenthesized — precedence becomes visible
  in tests and the fuzzer can check `parse(print(parse(x)))`.
- **Pratt parsing** (`internal/parser`): a prefix-function table, an infix table with binding
  powers, and `expr(bp)` — a dozen lines of loop. Syntax errors are `panic(*SyntaxError)`
  recovered **once per statement** in `stmt()` (the `text/template` trick: no error plumbing in
  any rule), then `sync()` skips to a statement boundary, stepping over whole `{...}` groups so
  the rest of a broken function does not cascade. `stmt` always consumes a token, so the parser
  always makes progress; `maxNesting` bounds recursion. `Parse` returns `errors.Join` of
  `*SyntaxError`, so `errors.As` finds the first.
- **Desugaring in the parser**: `&&`, `||`, `-x`, `!x` become `if`/`0 - x` (`binary`, `unary`),
  so neither engine, the compiler nor the VM has to know them. The price: `||` yields `true`
  rather than its left operand, and `-"a"` reports `type mismatch: int - string`.
- **Scoping, identical in both engines**: a block gets a scope only if it directly contains a
  `let`, and *all* its lets are declared up front (hoisted, `ast.Block.Lets`), so functions can
  be defined in any order and call each other; reading a hoisted name before its `let` ran is
  `variable used before its definition`. Each loop iteration enters a fresh scope, so closures
  capture a per-iteration binding. `eval.scoped` and `compiler.scoped` implement the same rule —
  the compile-time scope chain mirrors the runtime environment chain.
- **Tree-walker** (`internal/eval`): environments are small slices of bindings scanned
  linearly; `return` travels up the Go stack as an error value (`*returned`), which makes
  `return` inside an expression work for free.
- **Bytecode VM** (`internal/code`, `compiler`, `vm`): an `Instr{Op, A, B}` stream per function,
  a constant pool, a parallel position table (so runtime errors have `line:col`), an operand
  stack and a frame stack. Variables are addressed `(scopes up, slot)`. **A closure is code plus
  the environment it was created in** — simpler than flat closures with copied free variables
  (that is exercise 2), and mutation of captured variables is correct by construction. Jumps are
  patched after the target is known (`compiler.patch`).
- **Safety**: unbounded recursion stops at `object.MaxDepth` = 10,000 active calls with a
  language-level `stack overflow` error in *both* engines (`testdata/depth_edge.tl` pins the
  exact edge); `-max-steps` bounds calls + loop iterations for runaway programs; strings are
  capped at 16 MiB; arrays print cycle-safely. The fuzzers found no Go panic, hang or Go stack
  overflow.
- **Differential testing** (`tinylang_test.go`): every `testdata/*.tl` runs on both engines
  and must equal its golden `.out` — printed text, error message and `line:col` included.
  `FuzzEngines` extends that to mutated programs: whatever parses must produce the same output,
  value and error on both engines. `-update` rewrites goldens from the evaluator (then read the
  diff).
- **Parser fuzzing** (`parser_test.go`): `FuzzParse` — never panic, never hang, every error has
  a position — plus the round trip: an accepted program printed and re-parsed prints the same.
- **A REPL** (`cmd/tinylang/repl.go`): reads until `()`/`{}`/`[]` balance (counted on lexer
  tokens, so brackets in strings and comments are ignored), and the engine keeps its global
  scope between entries.

The language in a paragraph: semicolons are optional; `//` comments; ints are `int64` (wrap
on overflow, `/` truncates); strings are byte strings (`len` counts bytes, `s[i]` is a
one-byte string); only `false` and `null` are falsy; `==` compares ints/bools/strings by value
and arrays/functions by identity; a block's value is its last expression statement (else
`null`), which is what makes `if` an expression and gives functions implicit returns;
`return` may appear anywhere, even at top level. Builtins: `len`, `push` (mutates in place,
returns the array), `print`, `str`. Errors print as `3:9: runtime error: index out of range: 3
(length 3)` and exit 1.

## Run

```sh
go test -race ./...
go test -run TestDifferential -v .                      # both engines on every testdata/*.tl
go test -run xxx -fuzz FuzzParse -fuzztime 20s ./internal/parser
go test -run xxx -fuzz FuzzEngines -fuzztime 20s .
go test -run xxx -bench . -benchmem .                   # fib(25) and a 1M-iteration loop, eval vs vm

go run ./cmd/tinylang -example closures                 # embedded testdata/closures.tl, vm engine
go run ./cmd/tinylang -engine eval -example sort
go run ./cmd/tinylang -example err_index                # 3:9: runtime error: index out of range: 3 (length 3)
go run ./cmd/tinylang -example err_overflow             # 1:24: runtime error: stack overflow
go run ./cmd/tinylang -max-steps 100000 -example err_steps
printf 'let f = fn(n) {\n  n * 2\n}\nf(21)\n"a" + 1\n' | go run ./cmd/tinylang   # REPL
```

## Benchmarks and the optimization story

`BenchmarkFib25` runs `fib(25)` (242,785 calls); `BenchmarkLoop` runs
`while i < 1000000 { sum = sum + i % 7; i = i + 1 }`. Apple M4, shared with other builds, so
absolute numbers wobble ±10–15% between runs; each cell below is the median of 6 runs
(`-count=3`, run twice, baseline and optimized alternating). "Before" is the first
straightforward version of each engine, kept in a scratch copy; "after" is this repo.

| benchmark | before | after |
|---|---|---|
| `Fib25/eval` | 35.5 ms, 89.3 MB, 971,521 allocs | 17.8 ms, 19.4 MB, 728,732 allocs |
| `Fib25/vm`   | 15.9 ms, 19.4 MB, 728,763 allocs | 12.9 ms, 11.7 MB, 485,976 allocs |
| `Loop/eval`  | 97.7 ms, 24.0 MB, 2,999,660 allocs | 66.2 ms, 24.0 MB, 2,999,659 allocs |
| `Loop/vm`    | 49.6 ms, 16.0 MB, 1,999,677 allocs | 45.8 ms, 16.0 MB, 1,999,676 allocs |

`fib(30)` through the CLI (`time`, `-engine`): eval 0.19 s, vm 0.14 s.

Profiles: `go test -c -o /tmp/tl.test . && /tmp/tl.test -test.run xxx -test.bench
'^BenchmarkFib25/vm$' -test.benchtime=6s -test.cpuprofile vm.pprof`, then
`go tool pprof -top -focus=tinylang /tmp/tl.test vm.pprof` (same for `Fib25/eval`,
`Loop/vm`, `Loop/eval`). On macOS most samples land in idle runtime threads (`kevent`), so
`-focus=tinylang` is needed; the shares below are of the samples under tinylang frames.

What the profiles said, and what was done about it (VM steps are from one separate
`-count=5` series, hence a slightly different baseline than the table):

- **Eval, `Fib25`**: `newEnv` (a `map` per call) was 49% of allocated objects, and
  `makemap`/`memmove`/map access led the CPU samples. Replacing the `map[string]any` scope
  with a slice of `{name, val}` bindings scanned linearly (`eval.env`) took fib 39.2 → 17.8 ms
  and bytes/op 89 → 19 MB (loop 106 → 66 ms). Biggest win in the project, one type changed.
- **VM, `Fib25`**: the allocation profile showed `newEnv` (2 objects per call) and a third
  object per call, the heap-allocated `*frame`. Frames became values in a preallocated slice:
  allocs 728,763 → 485,976, 17.3 → 14.7 ms.
- **Tried and reverted**: putting up to four variables inline in `vm.env` to make it one
  allocation halved the allocs again (243,192) but doubled bytes/op (11.7 → 23.3 MB) and was
  *not faster* (14.9 ms). Allocation count is not time; the bigger zeroed, GC-scanned object
  cost what the saved allocation gained. Kept the simple version.
- **VM, both**: `object.Infix` showed up on every `+ - <`. An `int64` fast path in
  `OpBin` for those three (`vm.go`) took fib 14.7 → 13.4 ms and loop 50.2 → 46.3 ms. Reading the
  position table only when an error needs it (`frame.pos`) instead of on every instruction took
  another 13.4 → 12.9 and 46.3 → 45.2 ms.
- **What is left**: `Loop/vm` spends ~16% of its tinylang samples in `runtime.convT64`
  (the boxing): every `int64` result ≥ 256 is boxed into an `any`, two allocations per
  iteration. Avoiding that means a tagged `Value` struct instead of `any` (exercise 4).
- **The honest comparison**: with the tree-walker's map environment the VM was 2.2× faster
  on fib and 2.0× on the loop; once that was fixed the VM leads by 1.4× on both. The VM's
  design pays off less than the textbook says when the interpreter's data structures are the
  bottleneck, not the dispatch.

## Rewrite exercises

1. Rewrite the compiler and VM from memory: `compiler.go` (`stmts`, `scoped`, `expr`,
   `function`, jump patching) and the `Run` loop in `vm.go`, from the opcode table in
   `code.go` — then run `TestDifferential` against the evaluator.
2. Replace environment-pointer closures with flat closures: at `OpClosure` copy the free
   variables into the closure; boxes (or open/closed upvalues as in clox) keep mutation
   working. The compiler needs free-variable resolution; the golden tests tell you when
   `closures.tl` breaks.
3. Add element assignment `xs[i] = v`: parser (assignment target), evaluator, compiler
   (`OpSetIndex`), VM — drive it with new `testdata/*.tl` programs and let `FuzzEngines` look
   for disagreements.
4. Remove the boxing: replace `any` values with a small tagged `Value` struct (kind, int, pointer)
   in `internal/object`, rerun `BenchmarkLoop`, and see what the profile moves to.
5. Give `&&`/`||` real short-circuit opcodes that return their operand (`OpAnd`/`OpOr` with
   jump patching), delete the parser desugaring and implement them in both engines; or write a
   disassembler (`code.Fn.String`) and a `-dump` flag.

## Deliberately left out

- **Size**: the source is 1,795 lines by `wc -l` (1,524 without blank and comment lines), above
  the plan's ≈1,300-line guard. What already keeps it down: `&&`, `||`, `-`, `!` desugared in the
  parser, one shared `object` package, environment-pointer closures instead of flat closures,
  no element assignment, no disassembler, three-case VM fast path. Cutting further would mean
  dropping block scoping, the hand-written lexer or the robustness guards.
- No hashes, classes, modules (as planned); no floats; no `break`/`continue`; no element
  assignment (`xs[i] = v`), `pop`, slicing or string builtins beyond `len`/`str`/`print`/`push`.
- No `:engine` toggle in the REPL (choose with `-engine`).
- **Known difference, REPL only**: the VM resolves names when an entry is compiled, so a function
  may refer to a global defined by the same or an *earlier* entry; the evaluator looks names up
  when the function runs, so it also accepts a global defined by a *later* entry. Within one
  program (file) the engines are identical.
- Disassembler and `-dump` (exercise 5).
