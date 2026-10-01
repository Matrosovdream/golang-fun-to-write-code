package main

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const sample = `line one
TODO: fix this
line three
todo: lowercase
`

func TestSearchBasic(t *testing.T) {
	re := regexp.MustCompile("TODO")
	var out bytes.Buffer

	matched, err := search(&out, re, strings.NewReader(sample), "", options{})
	if err != nil {
		t.Fatal(err)
	}
	if !matched {
		t.Error("expected a match")
	}
	if got, want := out.String(), "TODO: fix this\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSearchIgnoreCaseAndLineNumbers(t *testing.T) {
	re := regexp.MustCompile("(?i)todo")
	var out bytes.Buffer

	_, err := search(&out, re, strings.NewReader(sample), "notes.txt", options{lineNums: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "notes.txt:2:TODO: fix this\nnotes.txt:4:todo: lowercase\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestSearchNoMatch(t *testing.T) {
	var out bytes.Buffer
	matched, err := search(&out, regexp.MustCompile("absent"), strings.NewReader(sample), "", options{})
	if err != nil {
		t.Fatal(err)
	}
	if matched || out.Len() != 0 {
		t.Errorf("expected no match and no output, got %q", out.String())
	}
}

func TestSearchAllRecursive(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"a.txt":       "has the WORD here\n",
		"sub/b.txt":   "nothing\n",
		"sub/c.txt":   "WORD again\n",
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	matched, errCount := searchAll(&out, regexp.MustCompile("WORD"), []string{dir}, options{recursive: true})
	if !matched || errCount != 0 {
		t.Fatalf("matched=%v errCount=%d, want true/0", matched, errCount)
	}
	for _, want := range []string{"a.txt", "c.txt"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %s:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "b.txt") {
		t.Errorf("b.txt should not match:\n%s", out.String())
	}
}
