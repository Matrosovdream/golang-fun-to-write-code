package site

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSplitFrontMatter(t *testing.T) {
	tests := []struct {
		name, doc, meta, body string
		wantErr               bool
	}{
		{name: "no front matter", doc: "# Hi\n", meta: "", body: "# Hi\n"},
		{name: "plain", doc: "---\ntitle: x\n---\nbody\n", meta: "title: x\n", body: "body\n"},
		{name: "empty block", doc: "---\n---\nbody\n", meta: "", body: "body\n"},
		{name: "closing fence at EOF", doc: "---\na: 1\n---", meta: "a: 1\n", body: ""},
		{name: "CRLF", doc: "---\r\na: 1\r\n---\r\nbody\r\n", meta: "a: 1\n", body: "body\n"},
		{name: "trailing space on fence", doc: "---\na: 1\n--- \nbody", meta: "a: 1\n", body: "body"},
		{name: "rule in the body is not a fence", doc: "---\na: 1\n---\ntext\n---\nmore\n", meta: "a: 1\n", body: "text\n---\nmore\n"},
		{name: "dashes without newline are not a fence", doc: "---x\nbody", meta: "", body: "---x\nbody"},
		{name: "unclosed", doc: "---\ntitle: x\nbody\n", wantErr: true},
		{name: "only an opening fence", doc: "---\n", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			meta, body, err := splitFrontMatter(tt.doc)
			if tt.wantErr {
				require.ErrorContains(t, err, "closing ---")
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.meta, meta)
			require.Equal(t, tt.body, body)
		})
	}
}

func TestParsePage(t *testing.T) {
	t.Run("fields, URL and normalised tags", func(t *testing.T) {
		src := "---\ntitle: Hi\nsummary: S\ndate: 2026-05-06\ntags: [Go, ' go ', Web Dev, '!!!']\ndraft: true\n---\n# body\n"
		p, err := parsePage("content/posts/Hello World.md", []byte(src))
		require.NoError(t, err)
		require.Equal(t, "Hi", p.Title)
		require.Equal(t, "S", p.Summary)
		require.Equal(t, "2026-05-06", p.Date.Format("2006-01-02"))
		require.Equal(t, []string{"go", "web-dev"}, p.Tags)
		require.True(t, p.Draft)
		require.True(t, p.IsPost)
		require.Equal(t, "/posts/hello-world/", p.URL)
		require.Equal(t, "# body\n", p.body)
	})

	t.Run("a page without front matter takes its title from the file name", func(t *testing.T) {
		p, err := parsePage("content/about.md", []byte("Just text.\n"))
		require.NoError(t, err)
		require.Equal(t, "about", p.Title)
		require.False(t, p.IsPost)
		require.Equal(t, "/about/", p.URL)
	})

	errTests := []struct{ name, src, want string }{
		{"malformed YAML", "---\ntitle: [unclosed\n---\n", "content/posts/x.md: front matter: yaml:"},
		{"unclosed fence", "---\ntitle: x\n", "content/posts/x.md: front matter is missing its closing ---"},
		{"unknown key", "---\ntitle: x\ndate: 2026-01-01\ntag: oops\n---\n", "field tag not found"},
		{"duplicate key", "---\ntitle: a\ntitle: b\ndate: 2026-01-01\n---\n", "already defined"},
		{"bad date", "---\ntitle: x\ndate: someday\n---\n", "parsing time"},
		{"post without date", "---\ntitle: x\n---\n", "content/posts/x.md: posts need a date"},
	}
	for _, tt := range errTests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parsePage("content/posts/x.md", []byte(tt.src))
			require.ErrorContains(t, err, tt.want)
			require.ErrorContains(t, err, "content/posts/x.md")
		})
	}
}
