package main

import (
	"encoding/json"
	"strings"
	"testing"
)

const doc = `{
  "name": "acme",
  "id": 9007199254740993,
  "users": [
    {"name": "alice", "admin": true},
    {"name": "bob", "admin": false}
  ]
}`

func TestExtract(t *testing.T) {
	parsed, err := decode(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want string
	}{
		{"name", "acme"},
		{"users.0.name", "alice"},
		{"users.1.admin", "false"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			got, err := extract(parsed, tc.path)
			if err != nil {
				t.Fatalf("extract: %v", err)
			}
			out, _ := pretty(got)
			if out != tc.want {
				t.Errorf("got %q, want %q", out, tc.want)
			}
		})
	}
}

func TestExtractErrorsNameThePath(t *testing.T) {
	parsed, _ := decode(strings.NewReader(doc))

	tests := []struct {
		path    string
		wantErr string
	}{
		{"nope", `key "nope" not found`},
		{"users.9.name", "out of range"},
		{"users.x", "not an array index"},
		{"name.deeper", "cannot descend into string"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			_, err := extract(parsed, tc.path)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("err = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// The reason decode uses UseNumber: 9007199254740993 does not fit float64
// exactly. With default decoding this test fails with ...992.
func TestLargeIntSurvives(t *testing.T) {
	parsed, _ := decode(strings.NewReader(doc))
	got, err := extract(parsed, "id")
	if err != nil {
		t.Fatal(err)
	}
	num, ok := got.(json.Number)
	if !ok {
		t.Fatalf("id is %T, want json.Number", got)
	}
	if num.String() != "9007199254740993" {
		t.Errorf("id = %s, precision lost", num)
	}
}

func TestRunPretty(t *testing.T) {
	out, err := run(strings.NewReader(`{"b":1,"a":[true,null]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": [\n    true,\n    null\n  ],\n  \"b\": 1\n}"
	if out != want {
		t.Errorf("got:\n%s\nwant:\n%s", out, want)
	}
}
