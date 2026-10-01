package main

import (
	"strings"
	"testing"
)

// Random output can't be compared to a fixed answer, so these are
// property tests: assert what must ALWAYS hold.

func TestPasswordLengthAndAlphabet(t *testing.T) {
	const alphabet = lower + digits
	for _, length := range []int{1, 8, 64} {
		p := password(alphabet, length)
		if len(p) != length {
			t.Errorf("len = %d, want %d", len(p), length)
		}
		for _, c := range p {
			if !strings.ContainsRune(alphabet, c) {
				t.Errorf("password %q contains %q, not in alphabet", p, c)
			}
		}
	}
}

func TestPasswordsAreDistinct(t *testing.T) {
	seen := make(map[string]bool)
	for range 100 {
		p := password(lower+upper+digits, 20)
		if seen[p] {
			t.Fatalf("duplicate password %q — randomness is broken", p)
		}
		seen[p] = true
	}
}

func TestPassphrase(t *testing.T) {
	words := []string{"alpha", "beta", "gamma"}

	p := passphrase(words, 4, "-")
	parts := strings.Split(p, "-")
	if len(parts) != 4 {
		t.Fatalf("got %d words, want 4 (%q)", len(parts), p)
	}
	for _, w := range parts {
		found := false
		for _, cand := range words {
			if w == cand {
				found = true
			}
		}
		if !found {
			t.Errorf("word %q not from the list", w)
		}
	}
}

func TestEmbeddedWordlist(t *testing.T) {
	words := strings.Fields(wordsFile)
	if len(words) < 100 {
		t.Fatalf("wordlist has %d words, want at least 100", len(words))
	}
	for _, w := range words {
		if w != strings.ToLower(w) {
			t.Errorf("word %q is not lowercase", w)
		}
	}
}

func TestRandIntBounds(t *testing.T) {
	for range 1000 {
		if v := randInt(3); v < 0 || v > 2 {
			t.Fatalf("randInt(3) = %d, out of range", v)
		}
	}
}
