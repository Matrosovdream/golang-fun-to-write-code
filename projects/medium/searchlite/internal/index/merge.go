package index

import "slices"

// Hit is a matching document with its score so far.
type Hit struct {
	Doc   int
	Score float64
}

// merge walks two lists sorted by Doc with one cursor each, O(len(a)+len(b)).
// The flags choose what survives: documents in both lists (scores add), only
// in a, only in b. Every boolean operator is one setting of the three.
func merge(a, b []Hit, both, onlyA, onlyB bool) []Hit {
	var out []Hit
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case j == len(b) || i < len(a) && a[i].Doc < b[j].Doc:
			if onlyA {
				out = append(out, a[i])
			}
			i++
		case i == len(a) || b[j].Doc < a[i].Doc:
			if onlyB {
				out = append(out, b[j])
			}
			j++
		default:
			if both {
				out = append(out, Hit{a[i].Doc, a[i].Score + b[j].Score})
			}
			i++
			j++
		}
	}
	return out
}

func intersect(a, b []Hit) []Hit  { return merge(a, b, true, false, false) }
func union(a, b []Hit) []Hit      { return merge(a, b, true, true, true) }
func difference(a, b []Hit) []Hit { return merge(a, b, false, true, false) }

// phrase finds the documents where term i sits offs[i] words after term 0,
// folding the lists left to right. The result keeps the start positions of
// term 0, so len(Pos) is the phrase's frequency in that document.
func phrase(lists [][]Posting, offs []int) []Posting {
	out := lists[0]
	for i := 1; i < len(lists); i++ {
		out = follow(out, lists[i], offs[i])
	}
	return out
}

// follow keeps the starts in a that have a partner from b exactly off words later.
func follow(a, b []Posting, off int) []Posting {
	var out []Posting
	for i, j := 0, 0; i < len(a) && j < len(b); {
		switch {
		case a[i].Doc < b[j].Doc:
			i++
		case a[i].Doc > b[j].Doc:
			j++
		default:
			var starts []int
			for _, p := range a[i].Pos {
				if _, ok := slices.BinarySearch(b[j].Pos, p+off); ok {
					starts = append(starts, p)
				}
			}
			if starts != nil {
				out = append(out, Posting{a[i].Doc, starts})
			}
			i++
			j++
		}
	}
	return out
}
