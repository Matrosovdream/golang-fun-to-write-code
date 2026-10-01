// quizcli asks questions from a CSV under a total time limit. Unanswered
// questions count as wrong when the clock runs out.
//
//	quizcli -limit 30s testdata/quiz.csv
package main

import (
	"bufio"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

type Question struct {
	Prompt string
	Answer string
}

func main() {
	limit := flag.Duration("limit", 30*time.Second, "total time limit")
	shuffle := flag.Bool("shuffle", false, "ask in random order")
	flag.Parse()

	path := "testdata/quiz.csv"
	if flag.NArg() > 0 {
		path = flag.Arg(0)
	}

	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "quizcli:", err)
		os.Exit(1)
	}
	questions, err := load(f)
	f.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "quizcli:", err)
		os.Exit(1)
	}
	if *shuffle {
		shuffleQuestions(questions)
	}

	fmt.Printf("%d questions, %v on the clock — go!\n", len(questions), *limit)
	score := run(os.Stdin, os.Stdout, questions, *limit)
	fmt.Printf("\nscore: %d / %d\n", score, len(questions))
}

func load(r io.Reader) ([]Question, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = 2

	if _, err := cr.Read(); err != nil { // header
		return nil, err
	}
	var qs []Question
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			return qs, nil
		}
		if err != nil {
			return nil, err
		}
		qs = append(qs, Question{Prompt: rec[0], Answer: rec[1]})
	}
}

// run is the concurrency lesson. Reading input BLOCKS — so a goroutine
// reads and forwards answers over a channel, and select races each answer
// against the shared deadline. This is the canonical "timeout on user
// input" shape; nothing simpler works.
func run(in io.Reader, w io.Writer, questions []Question, limit time.Duration) (score int) {
	answers := make(chan string)
	go func() {
		defer close(answers) // EOF (piped input ran out) closes the channel
		sc := bufio.NewScanner(in)
		for sc.Scan() {
			answers <- sc.Text()
		}
	}()

	// One deadline for the whole quiz: time.After starts the clock NOW,
	// outside the loop. Inside the loop it would reset per question.
	deadline := time.After(limit)

	for i, q := range questions {
		fmt.Fprintf(w, "\n%d) %s\n> ", i+1, q.Prompt)

		select {
		case ans, ok := <-answers:
			if !ok {
				fmt.Fprintln(w, "\nno more input — ending early")
				return score
			}
			if normalize(ans) == normalize(q.Answer) {
				fmt.Fprintln(w, "correct ✓")
				score++
			} else {
				fmt.Fprintf(w, "wrong — the answer is %q\n", q.Answer)
			}
		case <-deadline:
			fmt.Fprintln(w, "\n⏰ time's up!")
			return score
		}
	}
	return score
}

func normalize(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func shuffleQuestions(qs []Question) {
	rand.Shuffle(len(qs), func(i, j int) { qs[i], qs[j] = qs[j], qs[i] })
}
