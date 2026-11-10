// Package index is a positional inverted index: for every term, the sorted list
// of documents containing it and the word positions inside each. Everything a
// search needs (BM25, boolean merges, phrases, snippets) is derived from that.
//
// One RWMutex guards it all: Add takes the write lock; Search, Stats and Save
// take the read lock, so documents can be added while searches run.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"sync"

	"searchlite/internal/analysis"
)

var ErrExists = errors.New("document already indexed")

type Doc struct {
	Name, Title, Text string
	Len               int // indexed tokens: the document length in BM25
}

// Posting says where one term occurs in one document. len(Pos) is the term
// frequency; Pos is ascending and counts stopwords (see analysis.Token).
type Posting struct {
	Doc int
	Pos []int
}

type Index struct {
	mu       sync.RWMutex
	an       *analysis.Analyzer
	docs     []Doc
	byName   map[string]int
	postings map[string][]Posting // ascending by Doc, because docs are only appended
	tokens   int
}

type Option func(*Index)

// WithAnalyzer replaces the default analyzer. A snapshot does not record which
// analyzer built it, so Load must be given the same one.
func WithAnalyzer(a *analysis.Analyzer) Option { return func(ix *Index) { ix.an = a } }

func New(opts ...Option) *Index {
	ix := &Index{an: analysis.Default(), byName: map[string]int{}, postings: map[string][]Posting{}}
	for _, o := range opts {
		o(ix)
	}
	return ix
}

// Add indexes one document and returns its id. Names are unique and documents
// immutable: there is no update or delete.
func (ix *Index) Add(name, text string) (int, error) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if _, ok := ix.byName[name]; ok {
		return 0, fmt.Errorf("%w: %s", ErrExists, name)
	}
	id, n := len(ix.docs), 0
	positions := map[string][]int{}
	for t := range ix.an.Tokens(text) {
		positions[t.Term] = append(positions[t.Term], t.Pos)
		n++
	}
	for term, pos := range positions {
		ix.postings[term] = append(ix.postings[term], Posting{id, pos})
	}
	ix.docs = append(ix.docs, Doc{name, title(name, text), text, n})
	ix.byName[name] = id
	ix.tokens += n
	return id, nil
}

// title is the first markdown "# heading", or the name if there is none.
func title(name, text string) string {
	for line := range strings.Lines(text) {
		if h, ok := strings.CutPrefix(line, "# "); ok {
			return strings.TrimSpace(h)
		}
	}
	return name
}

// AddFS indexes every .md and .txt file under fsys, named by its path.
func (ix *Index) AddFS(fsys fs.FS) error {
	return fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(p) != ".md" && path.Ext(p) != ".txt" {
			return err
		}
		text, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		_, err = ix.Add(p, string(text))
		return err
	})
}

type Stats struct{ Docs, Terms, Tokens int }

func (ix *Index) Stats() Stats {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return Stats{len(ix.docs), len(ix.postings), ix.tokens}
}
