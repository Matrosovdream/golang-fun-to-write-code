package index

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"searchlite/internal/analysis"
	"searchlite/internal/corpus"
	"searchlite/internal/query"
)

// build indexes name/text pairs given as alternating arguments.
func build(t testing.TB, opts []Option, docs ...string) *Index {
	t.Helper()
	ix := New(opts...)
	for i := 0; i < len(docs); i += 2 {
		_, err := ix.Add(docs[i], docs[i+1])
		require.NoError(t, err)
	}
	return ix
}

func search(t testing.TB, ix *Index, q string) []Result {
	t.Helper()
	node, err := query.Parse(q)
	require.NoError(t, err, q)
	res, total := ix.Search(node, 100)
	require.Len(t, res, total)
	return res
}

func names(res []Result) []string {
	var out []string
	for _, r := range res {
		out = append(out, r.Name)
	}
	return out
}

func corpusIndex(t testing.TB) *Index {
	t.Helper()
	ix := New()
	require.NoError(t, ix.AddFS(corpus.FS()))
	return ix
}

func TestBM25SingleDocument(t *testing.T) {
	// N=1, df=1, tf=1, dl=avgdl: idf = ln(1 + 0.5/1.5) and the tf part is exactly 1.
	ix := build(t, nil, "a.md", "goroutine")
	res := search(t, ix, "goroutine")
	require.Len(t, res, 1)
	require.InDelta(t, math.Log(4.0/3), res[0].Score, 1e-12)
}

func TestRanking(t *testing.T) {
	filler := strings.Repeat("filler ", 40)
	ix := build(t, nil,
		"many.md", "goroutine goroutine goroutine channel",
		"once.md", "goroutine scheduler runtime memory",
		"long.md", "goroutine "+filler,
		"none.md", "channel buffer queue")

	// More occurrences beat fewer; a long document dilutes its single match.
	require.Equal(t, []string{"many.md", "once.md", "long.md"}, names(search(t, ix, "goroutine")))
	// "scheduler" is rare, so it outweighs the extra goroutine mentions elsewhere.
	require.Equal(t, "once.md", search(t, ix, "goroutine scheduler")[0].Name)
	// Scores come out best first.
	res := search(t, ix, "goroutine OR channel")
	for i := 1; i < len(res); i++ {
		require.GreaterOrEqual(t, res[i-1].Score, res[i].Score)
	}
}

func TestBooleanQueries(t *testing.T) {
	ix := build(t, nil,
		"0", "alpha beta", "1", "alpha gamma", "2", "beta gamma", "3", "alpha beta gamma", "4", "delta")
	tests := []struct {
		q    string
		want []string
	}{
		{"alpha", []string{"0", "1", "3"}},
		{"alpha beta", []string{"0", "3"}},
		{"alpha AND beta", []string{"0", "3"}},
		{"alpha OR delta", []string{"0", "1", "3", "4"}},
		{"alpha -beta", []string{"1"}},
		{"alpha NOT beta", []string{"1"}},
		{"-beta alpha", []string{"1"}},
		{"-alpha", []string{"2", "4"}},
		{"-alpha -beta", []string{"4"}},
		{"(alpha OR delta) beta", []string{"0", "3"}},
		{"alpha OR beta gamma", []string{"0", "1", "2", "3"}},
		{"alpha OR (beta gamma)", []string{"0", "1", "2", "3"}},
		{"(alpha OR beta) gamma", []string{"1", "2", "3"}},
		{`"alpha beta"`, []string{"0", "3"}},
		{`"beta alpha"`, nil},
		{`"beta gamma" -alpha`, []string{"2"}},
		{"nonexistent", nil},
		{"alpha nonexistent", nil},
		{"alpha OR nonexistent", []string{"0", "1", "3"}},
		{"alpha -nonexistent", []string{"0", "1", "3"}},
		{"the", nil},
		{"the alpha", []string{"0", "1", "3"}},
		{"alpha AND the", []string{"0", "1", "3"}},
		{"alpha -the", []string{"0", "1", "3"}},
		{"the -alpha", []string{"2", "4"}},
		{"-the", nil},
	}
	for _, tt := range tests {
		got := names(search(t, ix, tt.q))
		slices.Sort(got)
		require.Equal(t, tt.want, got, tt.q)
	}
}

func TestPhraseUsesPositions(t *testing.T) {
	ix := build(t, nil,
		"adjacent", "the memory model defines visibility",
		"reversed", "a model of memory usage",
		"apart", "memory is cheap but a model is not",
		"twice", "memory model memory model")

	require.ElementsMatch(t, []string{"adjacent", "twice"}, names(search(t, ix, `"memory model"`)))
	require.ElementsMatch(t, []string{"adjacent", "reversed", "apart", "twice"}, names(search(t, ix, `memory model`)))
	// A repeated phrase counts as a higher frequency, so it ranks first.
	require.Equal(t, "twice", search(t, ix, `"memory model"`)[0].Name)
}

func TestPhraseKeepsStopwordGaps(t *testing.T) {
	text := []string{"exact", "state of the art tools", "gap", "state the art tools"}
	ix := build(t, nil, text...)
	require.Equal(t, []string{"exact"}, names(search(t, ix, `"state of the art"`)))
	// The stopwords are dropped from both the query and the index, so the
	// distance between "state" and "art" is all that distinguishes the documents.
	require.ElementsMatch(t, []string{"exact", "gap"}, names(search(t, ix, `state art`)))

	// With no stopword filter the same phrase needs every word.
	plain := []Option{WithAnalyzer(analysis.New(analysis.Map(strings.ToLower)))}
	ix = build(t, plain, text...)
	require.Equal(t, []string{"exact"}, names(search(t, ix, `"state of the art"`)))
	require.ElementsMatch(t, []string{"exact", "gap"}, names(search(t, ix, `"the art"`)))
}

// The leapfrog intersection in phrase() against the obvious check: join each
// document's words with spaces and look for the phrase as a substring.
func TestPhraseMatchesNaive(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 8))
	word := func() string { return []string{"x", "y", "z"}[rng.IntN(3)] }
	for range 300 {
		docs := make([][]string, 1+rng.IntN(6))
		ix := New()
		for i := range docs {
			for range rng.IntN(15) {
				docs[i] = append(docs[i], word())
			}
			_, err := ix.Add(strconv.Itoa(i), strings.Join(docs[i], " "))
			require.NoError(t, err)
		}
		var ph []string
		for range 1 + rng.IntN(3) {
			ph = append(ph, word())
		}
		var want []string
		for i, d := range docs {
			if strings.Contains(" "+strings.Join(d, " ")+" ", " "+strings.Join(ph, " ")+" ") {
				want = append(want, strconv.Itoa(i))
			}
		}
		res, _ := ix.Search(query.Phrase{Text: strings.Join(ph, " ")}, 100)
		got := names(res)
		slices.Sort(got)
		require.Equal(t, want, got, "docs=%v phrase=%v", docs, ph)
	}
}

func TestAddDuplicate(t *testing.T) {
	ix := build(t, nil, "a.md", "one")
	_, err := ix.Add("a.md", "two")
	require.ErrorIs(t, err, ErrExists)
	require.Equal(t, Stats{Docs: 1, Terms: 1, Tokens: 1}, ix.Stats())
}

func TestTitle(t *testing.T) {
	ix := build(t, nil, "x.md", "intro\n# The Title \nbody", "y.txt", "no heading here")
	res := search(t, ix, "body OR heading")
	require.Equal(t, map[string]string{"x.md": "The Title", "y.txt": "y.txt"},
		map[string]string{res[0].Name: res[0].Title, res[1].Name: res[1].Title})
}

func TestSnippet(t *testing.T) {
	text := "# Heading\n\nThe scheduler parks goroutines. " + strings.Repeat("Filler words pad this out. ", 12) +
		"A memory model and the Memory Model again, plus one more goroutine."
	ix := build(t, nil, "d.md", text)
	snip := func(q string) string { return search(t, ix, q)[0].Snippet }

	// The window starts just before the first match; matches keep their spelling.
	require.Equal(t, "... words pad this out. A [[memory]] [[model]] and the [[Memory]] [[Model]] again, plus one more goroutine",
		snip(`"memory model"`))
	// Stemming: the query "goroutine" lights up "goroutines" too.
	require.Contains(t, snip("scheduler goroutine"), "The [[scheduler]] parks [[goroutines]]")
	// Words that are not an exact phrase hit stay plain.
	require.NotContains(t, snip(`"model memory" OR scheduler`), "[[memory]]")
	// Negated words are never highlighted.
	require.NotContains(t, snip("scheduler OR -filler"), "[[Filler]]")
	// With nothing to highlight the snippet is the start of the document.
	plain := snip("-zzz")
	require.True(t, strings.HasPrefix(plain, "Heading The scheduler parks goroutines. Filler") && strings.HasSuffix(plain, " ..."), plain)
}

func TestLiveIndexing(t *testing.T) {
	ix := New()
	const writers, perWriter = 4, 50
	q := query.Term{Text: "needle"}
	var stop atomic.Bool
	var wg, readers sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for i := range perWriter {
				_, err := ix.Add(fmt.Sprintf("w%d-%d", w, i), "a needle in haystack "+strconv.Itoa(i))
				assert.NoError(t, err)
			}
		})
	}
	for range 4 {
		readers.Go(func() {
			seen := 0
			for !stop.Load() {
				res, total := ix.Search(q, 5)
				assert.GreaterOrEqual(t, total, seen, "documents only ever get added")
				assert.LessOrEqual(t, len(res), 5)
				for _, r := range res {
					assert.Contains(t, r.Snippet, "[[needle]]")
				}
				seen = total
				ix.Stats()
			}
		})
	}
	wg.Wait()
	stop.Store(true)
	readers.Wait()
	_, total := ix.Search(q, 1)
	require.Equal(t, writers*perWriter, total)
}

func TestEmbeddedCorpus(t *testing.T) {
	ix := corpusIndex(t)
	require.InDelta(t, 17.5, ix.Stats().Docs, 2.5, "15-20 demo documents")

	tests := []struct {
		q    string
		want []string // names of the leading results, in order
	}{
		{`"memory model"`, []string{"memory-model.md", "atomics.md", "race-detector.md"}},
		{`goroutine AND "memory model" -mutex`, []string{"race-detector.md"}},
		{`channel`, []string{"channels.md"}},
		{`profiling`, []string{"profiling.md"}},
		{`mutexes`, []string{"mutexes.md"}},
	}
	for _, tt := range tests {
		got := names(search(t, ix, tt.q))
		require.GreaterOrEqual(t, len(got), len(tt.want), tt.q)
		require.Equal(t, tt.want, got[:len(tt.want)], tt.q)
	}
}
