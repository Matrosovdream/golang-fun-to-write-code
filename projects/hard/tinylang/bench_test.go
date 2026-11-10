package tinylang_test

import (
	"io"
	"testing"

	"github.com/stretchr/testify/require"

	"tinylang"
	"tinylang/internal/parser"
)

const (
	fibSrc  = `let fib = fn(n) { if n < 2 { n } else { fib(n - 1) + fib(n - 2) } }; fib(25)`
	loopSrc = `let i = 0; let sum = 0; while i < 1000000 { sum = sum + i % 7; i = i + 1 }; sum`
)

// go test -bench . -benchmem
func benchmark(b *testing.B, src string, want int64) {
	prog, err := parser.Parse(src)
	require.NoError(b, err)
	for _, engine := range engines {
		b.Run(engine, func(b *testing.B) {
			e, err := tinylang.NewEngine(engine, io.Discard, 0)
			require.NoError(b, err)
			b.ReportAllocs()
			for b.Loop() {
				v, err := e(prog)
				if err != nil || v != want {
					b.Fatalf("got %v, %v; want %d", v, err, want)
				}
			}
		})
	}
}

func BenchmarkFib25(b *testing.B) { benchmark(b, fibSrc, 75025) }
func BenchmarkLoop(b *testing.B)  { benchmark(b, loopSrc, 2999997) }
