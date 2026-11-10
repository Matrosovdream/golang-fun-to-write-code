package index

import (
	"io/fs"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"searchlite/internal/corpus"
	"searchlite/internal/query"
)

var queries = map[string]string{
	"term":    "goroutine",
	"or":      "channel OR mutex",
	"phrase":  `"memory model"`,
	"mixed":   `goroutine AND "memory model" -mutex`,
	"implied": "race detector memory",
}

func BenchmarkIndexCorpus(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		ix := New()
		if err := ix.AddFS(corpus.FS()); err != nil {
			b.Fatal(err)
		}
	}
}

func benchSearch(b *testing.B, ix *Index) {
	for name, q := range queries {
		node, err := query.Parse(q)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				ix.Search(node, 10)
			}
		})
	}
}

func BenchmarkSearchCorpus(b *testing.B) { benchSearch(b, corpusIndex(b)) }

// 20,000 documents of 100 words each, drawn at random from the demo
// corpus vocabulary: large enough that merges and the top-K heap show up.
func BenchmarkSearchLarge(b *testing.B) {
	var words []string
	err := fs.WalkDir(corpus.FS(), ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		text, err := fs.ReadFile(corpus.FS(), p)
		words = append(words, strings.Fields(string(text))...)
		return err
	})
	if err != nil {
		b.Fatal(err)
	}
	rng := rand.New(rand.NewPCG(1, 2))
	ix := New()
	for i := range 20_000 {
		var sb strings.Builder
		for range 100 {
			sb.WriteString(words[rng.IntN(len(words))] + " ")
		}
		if _, err := ix.Add(strconv.Itoa(i), sb.String()); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	benchSearch(b, ix)
}
