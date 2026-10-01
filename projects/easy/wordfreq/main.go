// wordfreq prints the most frequent words of its input.
//
//	wordfreq -top 10 book.txt
//	cat book.txt | wordfreq
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"unicode"
)

func main() {
	top := flag.Int("top", 10, "how many words to show")
	minLen := flag.Int("min", 1, "ignore words shorter than this")
	flag.Parse()

	// Files given → read them all; none → stdin. Everything downstream only
	// sees io.Reader, so the source makes no difference to the logic.
	var in io.Reader = os.Stdin
	if flag.NArg() > 0 {
		readers := make([]io.Reader, 0, flag.NArg())
		for _, name := range flag.Args() {
			f, err := os.Open(name)
			if err != nil {
				fmt.Fprintln(os.Stderr, "wordfreq:", err)
				os.Exit(1)
			}
			defer f.Close()
			readers = append(readers, f)
		}
		in = io.MultiReader(readers...)
	}

	if err := run(os.Stdout, in, *top, *minLen); err != nil {
		fmt.Fprintln(os.Stderr, "wordfreq:", err)
		os.Exit(1)
	}
}

func run(w io.Writer, r io.Reader, top, minLen int) error {
	counts, total, err := count(r, minLen)
	if err != nil {
		return err
	}

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "WORD\tCOUNT\tSHARE")
	for _, e := range topN(counts, top) {
		fmt.Fprintf(tw, "%s\t%d\t%.1f%%\n", e.word, e.count, float64(e.count)/float64(total)*100)
	}
	return tw.Flush()
}

// count tallies words, lowercased, split on anything that is not a letter,
// digit or apostrophe (so "don't" stays one word).
func count(r io.Reader, minLen int) (map[string]int, int, error) {
	counts := make(map[string]int)
	total := 0

	sc := bufio.NewScanner(r)
	for sc.Scan() {
		words := strings.FieldsFunc(sc.Text(), func(r rune) bool {
			return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '\''
		})
		for _, word := range words {
			word = strings.ToLower(strings.Trim(word, "'"))
			if word == "" || len([]rune(word)) < minLen {
				continue
			}
			counts[word]++
			total++
		}
	}
	return counts, total, sc.Err()
}

type entry struct {
	word  string
	count int
}

// topN sorts by count descending, then alphabetically — ties get a stable,
// testable order.
func topN(counts map[string]int, n int) []entry {
	entries := make([]entry, 0, len(counts))
	for word, c := range counts {
		entries = append(entries, entry{word, c})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].count != entries[j].count {
			return entries[i].count > entries[j].count
		}
		return entries[i].word < entries[j].word
	})
	if len(entries) > n {
		entries = entries[:n]
	}
	return entries
}
