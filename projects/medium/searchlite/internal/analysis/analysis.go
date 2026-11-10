// Package analysis turns text into index terms. An Analyzer is a tokenizer
// followed by a chain of Filters; every stage is one small func, so a
// pipeline is composed rather than configured.
package analysis

import (
	"iter"
	"strings"
	"unicode"
)

// Token is one word. Pos counts every word in the text, including those a
// filter later drops: removing a stopword leaves a gap in Pos instead of making
// its neighbours adjacent, which is what lets phrase queries stay exact.
// Start and End are byte offsets into the original text (used for snippets).
type Token struct {
	Term       string
	Pos        int
	Start, End int
}

// Filter rewrites a token or drops it (ok == false).
type Filter func(Token) (t Token, ok bool)

type Analyzer struct{ filters []Filter }

func New(filters ...Filter) *Analyzer { return &Analyzer{filters} }

// Default is the pipeline the index uses: lowercase, drop stopwords, stem.
func Default() *Analyzer {
	return New(Map(strings.ToLower), Drop(IsStopword), Map(Stem))
}

// Map lifts a string function into a Filter.
func Map(f func(string) string) Filter {
	return func(t Token) (Token, bool) {
		t.Term = f(t.Term)
		return t, true
	}
}

// Drop discards every token whose term satisfies pred.
func Drop(pred func(string) bool) Filter {
	return func(t Token) (Token, bool) { return t, !pred(t.Term) }
}

// Tokens splits text on anything that is not a letter or a digit and runs each
// word through the filters. It is lazy: callers that stop early do no extra work.
func (a *Analyzer) Tokens(text string) iter.Seq[Token] {
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }
	isGap := func(r rune) bool { return !isWord(r) }
	return func(yield func(Token) bool) {
		off, pos := 0, 0
		for {
			i := strings.IndexFunc(text[off:], isWord)
			if i < 0 {
				return
			}
			start := off + i
			n := strings.IndexFunc(text[start:], isGap)
			if n < 0 {
				n = len(text) - start
			}
			off = start + n
			tok, ok := a.apply(Token{text[start:off], pos, start, off})
			pos++
			if ok && !yield(tok) {
				return
			}
		}
	}
}

func (a *Analyzer) apply(t Token) (Token, bool) {
	for _, f := range a.filters {
		var ok bool
		if t, ok = f(t); !ok {
			return t, false
		}
	}
	return t, true
}

var stopwords = func() map[string]bool {
	set := map[string]bool{}
	for w := range strings.FieldsSeq("a an and are as at be but by for if in into is it no not of on or such that the their then there these they this to was will with") {
		set[w] = true
	}
	return set
}()

func IsStopword(w string) bool { return stopwords[w] }

// Stem is a light suffix stripper: plural -s/-es/-ies, then one of -ing/-ed/-ly
// (undoubling a final consonant: running -> run), then a trailing -e.
// It is a heuristic: it knows no irregular forms (mice, ran), merges unrelated
// words that share a stem (mode/mod), misses related ones (use/using,
// scheduler/schedule) and mangles some (analysis -> analysi). Index and query
// both go through it, so whatever it does, it does consistently.
func Stem(w string) string {
	if len(w) <= 3 {
		return w
	}
	switch {
	case strings.HasSuffix(w, "sses"):
		w = w[:len(w)-2]
	case strings.HasSuffix(w, "ies") && len(w) > 4:
		w = w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss"):
		w = w[:len(w)-1]
	}
	for _, suffix := range []string{"ing", "ed", "ly"} {
		if stem, ok := strings.CutSuffix(w, suffix); ok && len(stem) >= 3 && strings.ContainsAny(stem, "aeiouy") {
			w = undouble(stem)
			break
		}
	}
	if len(w) > 3 && strings.HasSuffix(w, "e") {
		w = w[:len(w)-1]
	}
	return w
}

func undouble(s string) string {
	n := len(s)
	if n >= 2 && s[n-1] == s[n-2] && strings.IndexByte("aeiouylsz", s[n-1]) < 0 {
		return s[:n-1]
	}
	return s
}
