package index

import (
	"slices"
	"strings"

	"searchlite/internal/analysis"
)

const window = 24 // tokens per snippet

// snippet shows a window of text starting just before the first match, with
// each matching word wrapped in [[ ]]. It re-analyses the text, so "goroutines"
// lights up for the query "goroutine"; a phrase lights up only where all its
// words sit at the right distances, exactly as the index matched it.
func (ix *Index) snippet(text string, phrases [][]analysis.Token) string {
	toks := slices.Collect(ix.an.Tokens(text))
	termAt := map[int]string{}
	for _, t := range toks {
		termAt[t.Pos] = t.Term
	}
	marked := map[int]bool{} // token positions inside a match
	for _, p := range phrases {
		for _, t := range toks {
			start := t.Pos - p[0].Pos
			if slices.ContainsFunc(p, func(w analysis.Token) bool { return termAt[start+w.Pos] != w.Term }) {
				continue
			}
			for _, w := range p {
				marked[start+w.Pos] = true
			}
		}
	}
	lo := max(slices.IndexFunc(toks, func(t analysis.Token) bool { return marked[t.Pos] })-3, 0)
	hi := min(lo+window, len(toks))
	var sb strings.Builder
	if lo > 0 {
		sb.WriteString("... ")
	}
	for i, t := range toks[lo:hi] {
		if i > 0 {
			sb.WriteString(text[toks[lo+i-1].End:t.Start])
		}
		word := text[t.Start:t.End]
		if marked[t.Pos] {
			word = "[[" + word + "]]"
		}
		sb.WriteString(word)
	}
	if hi < len(toks) {
		sb.WriteString(" ...")
	}
	return strings.Join(strings.Fields(sb.String()), " ")
}
