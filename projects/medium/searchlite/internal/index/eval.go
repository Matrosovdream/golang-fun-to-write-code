package index

import (
	"math"
	"slices"

	"searchlite/internal/analysis"
	"searchlite/internal/query"
	"searchlite/internal/topk"
)

const (
	k1 = 1.2  // term-frequency saturation
	b  = 0.75 // how strongly long documents are penalised
)

type Result struct {
	Name    string  `json:"name"`
	Title   string  `json:"title"`
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet"`
}

// Search returns the k best documents for q, best first, and how many
// documents matched in total.
func (ix *Index) Search(q query.Node, k int) (results []Result, total int) {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	results = []Result{}
	hits, ok := ix.eval(q)
	if !ok {
		return results, 0
	}
	best := topk.New(k, func(a, b Hit) bool {
		if a.Score != b.Score {
			return a.Score < b.Score
		}
		return a.Doc > b.Doc // equal scores: the earlier document wins
	})
	for _, h := range hits {
		best.Push(h)
	}
	var phrases [][]analysis.Token
	for _, text := range query.Positive(q) {
		if toks := slices.Collect(ix.an.Tokens(text)); len(toks) > 0 {
			phrases = append(phrases, toks)
		}
	}
	for _, h := range best.Drain() {
		d := ix.docs[h.Doc]
		results = append(results, Result{d.Name, d.Title, h.Score, ix.snippet(d.Text, phrases)})
	}
	return results, len(hits)
}

// eval returns the matching documents in ascending order. ok == false means
// the subtree says nothing, because it analysed to no terms at all (a query of
// only stopwords); its parent then ignores it instead of matching nothing.
func (ix *Index) eval(n query.Node) (hits []Hit, ok bool) {
	switch n := n.(type) {
	case query.Term:
		return ix.leaf(n.Text)
	case query.Phrase:
		return ix.leaf(n.Text)
	case query.And:
		return ix.both(n.L, n.R, intersect)
	case query.Or:
		return ix.both(n.L, n.R, union)
	case query.Not: // everything except x, unscored
		x, ok := ix.eval(n.X)
		return difference(ix.all(), x), ok
	}
	panic("unknown query node")
}

func (ix *Index) both(l, r query.Node, merge func(a, b []Hit) []Hit) ([]Hit, bool) {
	a, aok := ix.eval(l)
	c, cok := ix.eval(r)
	switch {
	case !aok:
		return c, cok
	case !cok:
		return a, true
	}
	return merge(a, c), true
}

func (ix *Index) all() []Hit {
	hits := make([]Hit, len(ix.docs))
	for i := range hits {
		hits[i].Doc = i
	}
	return hits
}

// leaf matches a word or a quoted phrase: both are a run of analysed tokens
// that must keep their original distances. One token is just a term lookup.
func (ix *Index) leaf(text string) ([]Hit, bool) {
	toks := slices.Collect(ix.an.Tokens(text))
	if len(toks) == 0 {
		return nil, false
	}
	lists := make([][]Posting, len(toks))
	offs := make([]int, len(toks))
	for i, t := range toks {
		lists[i] = ix.postings[t.Term]
		offs[i] = t.Pos - toks[0].Pos
	}
	ps := phrase(lists, offs)
	hits := make([]Hit, len(ps))
	for i, p := range ps {
		hits[i] = Hit{p.Doc, ix.bm25(len(ps), len(p.Pos), ix.docs[p.Doc].Len)}
	}
	return hits, true
}

// bm25 scores one (term or phrase, document) pair. df is the number of
// documents containing it, tf its count in this one, dl this document's length.
// The idf is the "+1" variant, which stays positive for very common terms.
func (ix *Index) bm25(df, tf, dl int) float64 {
	n := float64(len(ix.docs))
	avgdl := float64(ix.tokens) / n
	idf := math.Log(1 + (n-float64(df)+0.5)/(float64(df)+0.5))
	f := float64(tf)
	return idf * f * (k1 + 1) / (f + k1*(1-b+b*float64(dl)/avgdl))
}
