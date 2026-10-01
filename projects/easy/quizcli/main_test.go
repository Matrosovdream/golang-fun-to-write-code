package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"
)

var qs = []Question{
	{"2+2?", "4"},
	{"capital of France?", "Paris"},
	{"zero value of int?", "0"},
}

func TestLoad(t *testing.T) {
	csv := "question,answer\n2+2?,4\ncapital?,Paris\n"
	got, err := load(strings.NewReader(csv))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Answer != "Paris" {
		t.Errorf("load = %+v", got)
	}
}

func TestScoringNormalizesAnswers(t *testing.T) {
	// "  PARIS " must count: normalize trims and lowercases both sides.
	input := "4\n  PARIS \nwrong\n"
	var out bytes.Buffer

	score := run(strings.NewReader(input), &out, qs, time.Second)
	if score != 2 {
		t.Errorf("score = %d, want 2\noutput:\n%s", score, out.String())
	}
	if !strings.Contains(out.String(), `the answer is "0"`) {
		t.Errorf("wrong answer not revealed:\n%s", out.String())
	}
}

func TestInputEndingEarlyStopsQuiz(t *testing.T) {
	var out bytes.Buffer
	score := run(strings.NewReader("4\n"), &out, qs, time.Second)
	if score != 1 {
		t.Errorf("score = %d, want 1", score)
	}
	if !strings.Contains(out.String(), "no more input") {
		t.Errorf("missing early-end message:\n%s", out.String())
	}
}

func TestDeadlineEndsQuiz(t *testing.T) {
	// A pipe with no writer never delivers input and never EOFs —
	// exactly like a person staring at the terminal. Only the deadline
	// can end this quiz.
	r, w := io.Pipe()
	defer w.Close()

	var out bytes.Buffer
	start := time.Now()
	score := run(r, &out, qs, 60*time.Millisecond)

	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("quiz took %v, deadline did not fire", elapsed)
	}
	if score != 0 {
		t.Errorf("score = %d, want 0", score)
	}
	if !strings.Contains(out.String(), "time's up") {
		t.Errorf("missing timeout message:\n%s", out.String())
	}
}
