package main

import (
	"bytes"
	"flag"
	"os"
	"reflect"
	"strings"
	"testing"
)

// -update rewrites the golden file instead of comparing against it:
//
//	go test -update
var update = flag.Bool("update", false, "rewrite golden files")

func TestCount(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		minLen int
		want   map[string]int
	}{
		{
			name:  "lowercases and splits on punctuation",
			input: "Go, go GO! gophers go...",
			want:  map[string]int{"go": 4, "gophers": 1},
		},
		{
			name:  "keeps apostrophes inside words",
			input: "don't don't do",
			want:  map[string]int{"don't": 2, "do": 1},
		},
		{
			name:   "min length filters short words",
			input:  "a bb ccc dddd",
			minLen: 3,
			want:   map[string]int{"ccc": 1, "dddd": 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _, err := count(strings.NewReader(tc.input), tc.minLen)
			if err != nil {
				t.Fatalf("count: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTopNStableOrder(t *testing.T) {
	counts := map[string]int{"banana": 2, "apple": 2, "cherry": 5}

	got := topN(counts, 3)
	want := []entry{{"cherry", 5}, {"apple", 2}, {"banana", 2}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRunGolden(t *testing.T) {
	input := `The quick brown fox jumps over the lazy dog.
The dog barks. The fox runs. Quick, quick!`

	var buf bytes.Buffer
	if err := run(&buf, strings.NewReader(input), 3, 1); err != nil {
		t.Fatalf("run: %v", err)
	}

	golden := "testdata/top3.golden"
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatalf("update golden: %v", err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run `go test -update` once): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", buf.Bytes(), want)
	}
}
