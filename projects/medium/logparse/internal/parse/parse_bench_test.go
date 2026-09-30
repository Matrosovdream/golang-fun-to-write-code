package parse_test

import (
	"testing"

	"logparse/internal/parse"
)

// Run with:
//
//	go test -bench=. -benchmem ./internal/parse
//
// -benchmem shows B/op and allocs/op — for parsers the allocation count
// usually explains the speed difference before CPU profiles do.
func BenchmarkParseNaive(b *testing.B) {
	for b.Loop() {
		if _, err := parse.ParseNaive(sample); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseFast(b *testing.B) {
	for b.Loop() {
		if _, err := parse.ParseFast(sample); err != nil {
			b.Fatal(err)
		}
	}
}
