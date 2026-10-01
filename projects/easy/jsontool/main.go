// jsontool pretty-prints JSON and extracts values by dotted path.
//
//	cat data.json | jsontool
//	jsontool -path users.0.name data.json
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func main() {
	args := os.Args[1:]
	path := ""
	if len(args) >= 2 && args[0] == "-path" {
		path = args[1]
		args = args[2:]
	}

	in := io.Reader(os.Stdin)
	if len(args) > 0 {
		f, err := os.Open(args[0])
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		in = f
	}

	out, err := run(in, path)
	if err != nil {
		fatal(err)
	}
	fmt.Println(out)
}

func run(r io.Reader, path string) (string, error) {
	doc, err := decode(r)
	if err != nil {
		return "", fmt.Errorf("parse json: %w", err)
	}

	if path != "" {
		doc, err = extract(doc, path)
		if err != nil {
			return "", err
		}
	}
	return pretty(doc)
}

// decode parses into any: objects become map[string]any, arrays []any,
// strings string, booleans bool, null nil — and numbers json.Number
// because of UseNumber. Without it every number is float64, and large
// int64 IDs silently lose precision.
func decode(r io.Reader) (any, error) {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var doc any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// extract walks "users.0.name": map keys and array indexes, split on dots.
// The type switch is the whole lesson: dynamic JSON is navigated by
// asserting what each level actually is.
func extract(doc any, path string) (any, error) {
	cur := doc
	for i, part := range strings.Split(path, ".") {
		where := strings.Join(strings.Split(path, ".")[:i+1], ".")

		switch node := cur.(type) {
		case map[string]any:
			child, ok := node[part]
			if !ok {
				return nil, fmt.Errorf("%s: key %q not found", where, part)
			}
			cur = child
		case []any:
			idx, err := strconv.Atoi(part)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an array index", where, part)
			}
			if idx < 0 || idx >= len(node) {
				return nil, fmt.Errorf("%s: index %d out of range (len %d)", where, idx, len(node))
			}
			cur = node[idx]
		default:
			return nil, fmt.Errorf("%s: cannot descend into %T", where, cur)
		}
	}
	return cur, nil
}

func pretty(doc any) (string, error) {
	// A bare string result prints without quotes — nicer for shell use.
	if s, ok := doc.(string); ok {
		return s, nil
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	return string(raw), err
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "jsontool:", err)
	os.Exit(1)
}
