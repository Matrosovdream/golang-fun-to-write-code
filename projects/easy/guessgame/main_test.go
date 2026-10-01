package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlayScriptedGame(t *testing.T) {
	// Binary search for 62: 50 → higher, 75 → lower, 62 → correct.
	// "abc" and "999" are invalid and must re-prompt without counting.
	input := "50\nabc\n999\n75\n62\n"
	var out bytes.Buffer

	attempts, err := play(strings.NewReader(input), &out, 62, 100)
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3 (invalid input must not count)", attempts)
	}

	for _, want := range []string{"higher", "lower", "correct! 62 in 3 attempts", "please enter a number"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestPlayInputEndsEarly(t *testing.T) {
	var out bytes.Buffer
	_, err := play(strings.NewReader("10\n"), &out, 50, 100)
	if err == nil {
		t.Fatal("expected an error when input ends before winning")
	}
}

func TestUpdateBest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "best")

	if best, improved := updateBest(path, 7); !improved || best != 7 {
		t.Errorf("first game: best=%d improved=%v, want 7/true", best, improved)
	}
	if best, improved := updateBest(path, 9); improved || best != 7 {
		t.Errorf("worse game: best=%d improved=%v, want 7/false", best, improved)
	}
	if best, improved := updateBest(path, 4); !improved || best != 4 {
		t.Errorf("better game: best=%d improved=%v, want 4/true", best, improved)
	}
}
