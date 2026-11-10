package dag

import (
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Parse reads a task file:
//
//	tasks:
//	  build: {cmd: go build ./..., deps: [gen], env: {CGO_ENABLED: "0"}, dir: .}
func Parse(r io.Reader) (*Graph, error) {
	var file struct {
		Tasks map[string]Task `yaml:"tasks"`
	}
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true) // a typo like `dep:` should fail loudly, not silently drop edges
	if err := dec.Decode(&file); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse task file: %w", err)
	}
	return New(file.Tasks)
}

func Load(path string) (*Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("load task file: %w", err)
	}
	defer f.Close()
	g, err := Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return g, nil
}
