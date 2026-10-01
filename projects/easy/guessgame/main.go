// guessgame: the program picks a number, you narrow it down.
package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strconv"
	"strings"
)

func main() {
	max := 100
	if len(os.Args) > 1 {
		if v, err := strconv.Atoi(os.Args[1]); err == nil && v > 1 {
			max = v
		}
	}

	secret := rand.IntN(max) + 1
	attempts, err := play(os.Stdin, os.Stdout, secret, max)
	if err != nil {
		fmt.Fprintln(os.Stderr, "guessgame:", err)
		os.Exit(1)
	}

	best, improved := updateBest(scorePath(), attempts)
	if improved {
		fmt.Printf("🏆 new best: %d attempts!\n", attempts)
	} else {
		fmt.Printf("best so far: %d attempts\n", best)
	}
}

// play runs the guessing loop. It talks only to the given reader/writer, so
// tests can script an entire game.
func play(r io.Reader, w io.Writer, secret, max int) (attempts int, err error) {
	fmt.Fprintf(w, "I picked a number between 1 and %d.\n", max)

	sc := bufio.NewScanner(r)
	for {
		fmt.Fprint(w, "your guess: ")
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return attempts, err
			}
			return attempts, errors.New("input ended before you found it")
		}

		guess, err := strconv.Atoi(strings.TrimSpace(sc.Text()))
		if err != nil || guess < 1 || guess > max {
			// Invalid input re-prompts and does NOT count as an attempt.
			fmt.Fprintf(w, "please enter a number from 1 to %d\n", max)
			continue
		}

		attempts++
		switch {
		case guess < secret:
			fmt.Fprintln(w, "higher ↑")
		case guess > secret:
			fmt.Fprintln(w, "lower ↓")
		default:
			fmt.Fprintf(w, "correct! %d in %d attempts\n", secret, attempts)
			return attempts, nil
		}
	}
}

// updateBest persists the best (lowest) attempt count in a tiny score file
// and reports whether this game beat it.
func updateBest(path string, attempts int) (best int, improved bool) {
	best = attempts
	improved = true

	if raw, err := os.ReadFile(path); err == nil {
		if prev, err := strconv.Atoi(strings.TrimSpace(string(raw))); err == nil && prev <= attempts {
			return prev, false
		}
	}
	// Either no score yet or we beat it — both end up here.
	_ = os.WriteFile(path, []byte(strconv.Itoa(attempts)), 0o644)
	return best, improved
}

func scorePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".guessgame_best"
	}
	return home + "/.guessgame_best"
}
