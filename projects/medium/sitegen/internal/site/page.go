package site

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

type FrontMatter struct {
	Title   string    `yaml:"title"`
	Summary string    `yaml:"summary"`
	Date    time.Time `yaml:"date"`
	Tags    []string  `yaml:"tags"`
	Draft   bool      `yaml:"draft"`
}

// Page is one Markdown source file. The body is only converted to HTML at
// render time, so an unchanged page never pays for it.
type Page struct {
	FrontMatter
	URL    string // "/posts/hello/"
	IsPost bool

	body string
	hash string // sha256 of the source bytes
}

// parsePage turns content/posts/hello.md into a Page served at /posts/hello/.
func parsePage(name string, src []byte) (*Page, error) {
	meta, body, err := splitFrontMatter(string(src))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	p := &Page{body: body, hash: fmt.Sprintf("%x", sha256.Sum256(src))}
	if err := decodeYAML([]byte(meta), &p.FrontMatter); err != nil {
		return nil, fmt.Errorf("%s: front matter: %w", name, err)
	}

	rel := strings.TrimSuffix(strings.TrimPrefix(name, "content/"), ".md")
	segments := strings.Split(rel, "/")
	for i, s := range segments {
		segments[i] = slug(s)
	}
	p.URL = "/" + strings.Join(segments, "/") + "/"
	p.IsPost = strings.HasPrefix(rel, "posts/")
	p.Title = cmp.Or(p.Title, path.Base(rel))
	if p.IsPost && p.Date.IsZero() {
		return nil, fmt.Errorf("%s: posts need a date in the front matter", name)
	}
	for i, t := range p.Tags {
		p.Tags[i] = slug(t)
	}
	slices.Sort(p.Tags)
	p.Tags = slices.Compact(slices.DeleteFunc(p.Tags, func(t string) bool { return t == "" }))
	return p, nil
}

// splitFrontMatter hand-splits a "---" fenced YAML block off the top of a
// document. A file that does not start with a fence has no front matter.
func splitFrontMatter(doc string) (meta, body string, err error) {
	doc = strings.ReplaceAll(doc, "\r\n", "\n")
	rest, ok := strings.CutPrefix(doc, "---\n")
	if !ok {
		return "", doc, nil
	}
	for off := 0; off < len(rest); {
		line, _, _ := strings.Cut(rest[off:], "\n")
		if strings.TrimRight(line, " \t") == "---" {
			return rest[:off], rest[min(off+len(line)+1, len(rest)):], nil
		}
		off += len(line) + 1
	}
	return "", "", errors.New("front matter is missing its closing ---")
}

// decodeYAML rejects unknown keys, so a typo like "tag:" is an error instead
// of silently untagged content. An empty document is fine.
func decodeYAML(data []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(v); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// slug lower-cases s and turns every run of non-alphanumerics into one dash,
// so it is safe in a URL path.
func slug(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	return strings.Join(words, "-")
}
