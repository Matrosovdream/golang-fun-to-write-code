package site

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Paths lists everything written so far, sorted.
func (m *MemOutput) Paths() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Sorted(maps.Keys(m.files))
}

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func post(title, date string, tags ...string) *fstest.MapFile {
	return file(fmt.Sprintf("---\ntitle: %q\ndate: %s\ntags: [%s]\n---\nBody of %s.\n",
		title, date, strings.Join(tags, ", "), title))
}

func baseSite() fstest.MapFS {
	return fstest.MapFS{
		"sitegen.yaml":           file("title: Test Site\nbaseURL: https://example.com/\n"),
		"content/about.md":       file("---\ntitle: About\n---\nAbout me.\n"),
		"content/posts/alpha.md": post("Alpha", "2026-01-10", "go", "web"),
		"content/posts/beta.md":  post("Beta", "2026-02-20", "go"),
		"content/posts/gamma.md": post("Gamma", "2026-03-05", "web"),
		"content/posts/wip.md":   file("---\ntitle: WIP\ndate: 2026-04-01\ntags: [go, secret]\ndraft: true\n---\nunfinished\n"),
		"static/robots.txt":      file("User-agent: *\n"),
	}
}

func build(t *testing.T, src fstest.MapFS, opts ...Option) (*MemOutput, Stats) {
	t.Helper()
	out := NewMemOutput()
	st, err := New(opts...).Build(t.Context(), src, out)
	require.NoError(t, err)
	return out, st
}

func read(t *testing.T, out *MemOutput, path string) string {
	t.Helper()
	data, ok := out.Get(path)
	require.True(t, ok, "%s was not written; have %v", path, out.Paths())
	return string(data)
}

func TestBuildWritesEveryOutput(t *testing.T) {
	out, st := build(t, baseSite())

	want := []string{
		"about/index.html", "feed.xml", "index.html",
		"posts/alpha/index.html", "posts/beta/index.html", "posts/gamma/index.html",
		"robots.txt", "sitemap.xml", "style.css", "tags/go/index.html", "tags/web/index.html",
	}
	require.Equal(t, want, out.Paths())
	require.Equal(t, Stats{Rendered: len(want)}, st)

	index := read(t, out, "index.html")
	require.Less(t, strings.Index(index, "Gamma"), strings.Index(index, "Alpha"), "newest post first")
	require.Contains(t, read(t, out, "tags/go/index.html"), "Beta")
	require.NotContains(t, read(t, out, "tags/go/index.html"), "Gamma")
	require.Equal(t, "User-agent: *\n", read(t, out, "robots.txt"))
	require.Contains(t, read(t, out, "style.css"), "font")
}

func TestDraftsAreExcludedUnlessEnabled(t *testing.T) {
	out, _ := build(t, baseSite())
	for _, p := range []string{"index.html", "feed.xml", "sitemap.xml", "tags/go/index.html"} {
		require.NotContains(t, read(t, out, p), "WIP", p)
	}
	_, ok := out.Get("posts/wip/index.html")
	require.False(t, ok)
	_, ok = out.Get("tags/secret/index.html")
	require.False(t, ok)

	out, _ = build(t, baseSite(), WithDrafts(true))
	for _, p := range []string{"index.html", "feed.xml", "tags/go/index.html", "posts/wip/index.html"} {
		require.Contains(t, read(t, out, p), "WIP", p)
	}
	require.Contains(t, read(t, out, "sitemap.xml"), "/posts/wip/")
	require.Contains(t, read(t, out, "index.html"), `<span class="draft">`)
	require.Contains(t, read(t, out, "tags/secret/index.html"), "WIP")
}

func TestGoldenPost(t *testing.T) {
	src := baseSite()
	src["content/posts/golden.md"] = file("---\ntitle: Golden\ndate: 2026-05-01\ntags: [go]\nsummary: Shown in lists.\n---\n" +
		"## Heading\n\nSome *emphasis*, ~~strike~~ and `code`.\n\n" +
		"| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n\n" +
		"```go\nfmt.Println(\"<hi>\")\n```\n\nraw <b>html</b> is dropped; https://example.com autolinks.\n")
	out, _ := build(t, src)
	got := read(t, out, "posts/golden/index.html")

	golden := filepath.Join("testdata", "post.golden.html")
	if *update {
		require.NoError(t, os.MkdirAll("testdata", 0o755))
		require.NoError(t, os.WriteFile(golden, []byte(got), 0o644))
	}
	want, err := os.ReadFile(golden)
	require.NoError(t, err, "run: go test ./internal/site -update")
	require.Equal(t, string(want), got)
}

func TestHTMLIsEscaped(t *testing.T) {
	src := baseSite()
	src["content/posts/xss.md"] = file("---\ntitle: <script>alert(1)</script>\ndate: 2026-06-01\ntags: [go]\n---\nhello <script>steal()</script> world\n")
	out, _ := build(t, src)

	for _, p := range []string{"posts/xss/index.html", "index.html", "tags/go/index.html"} {
		html := read(t, out, p)
		require.Contains(t, html, "&lt;script&gt;alert(1)&lt;/script&gt;", p)
		require.NotContains(t, html, "<script>", p)
	}
	require.Contains(t, read(t, out, "posts/xss/index.html"), "<title>&lt;script&gt;alert(1)&lt;/script&gt; - Test Site</title>")
	require.Contains(t, read(t, out, "posts/xss/index.html"), "<!-- raw HTML omitted -->", "raw HTML in Markdown is dropped")
}

func TestSiteTemplatesOverrideTheTheme(t *testing.T) {
	src := baseSite()
	src["templates/page.html"] = file(`{{define "content"}}<p>CUSTOM {{.Page.Title}}</p>{{.Body}}{{end}}`)
	out, _ := build(t, src)

	about := read(t, out, "about/index.html")
	require.Contains(t, about, "<p>CUSTOM About</p>")
	require.Contains(t, about, "About me.", "only the layout is replaced; base.html still wraps it")
	require.Contains(t, about, "<footer>", "base.html still wraps it")
	require.NotContains(t, read(t, out, "posts/alpha/index.html"), "CUSTOM")
}

func TestBuildErrorsNameTheFile(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(fstest.MapFS)
		want   []string
	}{
		{"malformed YAML", func(m fstest.MapFS) { m["content/posts/bad.md"] = file("---\ntitle: [oops\n---\n") },
			[]string{"content/posts/bad.md", "front matter"}},
		{"missing closing fence", func(m fstest.MapFS) { m["content/posts/bad.md"] = file("---\ntitle: x\n") },
			[]string{"content/posts/bad.md", "closing ---"}},
		{"two bad files are both reported", func(m fstest.MapFS) {
			m["content/posts/bad1.md"] = file("---\ntitle: [oops\n---\n")
			m["content/posts/bad2.md"] = file("---\ntitle: x\n")
		}, []string{"content/posts/bad1.md", "content/posts/bad2.md"}},
		{"bad sitegen.yaml", func(m fstest.MapFS) { m["sitegen.yaml"] = file("titel: typo\n") },
			[]string{"sitegen.yaml", "titel"}},
		{"template syntax error", func(m fstest.MapFS) { m["templates/post.html"] = file("{{define \"content\"}}{{end") },
			[]string{"templates/post.html"}},
		{"no content directory", func(m fstest.MapFS) {
			for name := range m {
				if strings.HasPrefix(name, "content/") {
					delete(m, name)
				}
			}
		}, []string{"content"}},
		{"template references a missing field", func(m fstest.MapFS) {
			m["templates/page.html"] = file(`{{define "content"}}{{.Page.Nope}}{{end}}`)
		}, []string{"render about/index.html", "Nope"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := baseSite()
			tt.mutate(src)
			_, err := New().Build(t.Context(), src, NewMemOutput())
			require.Error(t, err)
			for _, w := range tt.want {
				require.ErrorContains(t, err, w)
			}
		})
	}
}

// spyOutput records which files a build actually wrote or removed.
type spyOutput struct {
	*MemOutput
	mu            sync.Mutex
	written, gone []string
	failWriteOnce string
}

func newSpy() *spyOutput { return &spyOutput{MemOutput: NewMemOutput()} }

func (s *spyOutput) Write(path string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if path == s.failWriteOnce {
		s.failWriteOnce = ""
		return errors.New("disk full")
	}
	s.written = append(s.written, path)
	return s.MemOutput.Write(path, data)
}

func (s *spyOutput) Remove(path string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gone = append(s.gone, path)
	return s.MemOutput.Remove(path)
}

// take returns what was written since the last call, sorted.
func (s *spyOutput) take() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := slices.Sorted(slices.Values(s.written))
	s.written = nil
	return w
}

func TestIncrementalBuild(t *testing.T) {
	src := baseSite()
	b := New()
	out := newSpy()
	rebuild := func() Stats {
		t.Helper()
		st, err := b.Build(t.Context(), src, out)
		require.NoError(t, err)
		return st
	}

	st := rebuild()
	total := st.Rendered
	require.Equal(t, Stats{Rendered: 11}, st)
	require.Len(t, out.take(), total)

	t.Run("nothing changed: nothing renders", func(t *testing.T) {
		require.Equal(t, Stats{Skipped: total}, rebuild())
		require.Empty(t, out.take())
	})

	t.Run("one post changed: that post and the pages listing it", func(t *testing.T) {
		src["content/posts/gamma.md"] = file(string(src["content/posts/gamma.md"].Data) + "Edited.\n")
		st := rebuild()
		require.Equal(t, []string{"feed.xml", "index.html", "posts/gamma/index.html", "sitemap.xml", "tags/web/index.html"}, out.take())
		require.Equal(t, Stats{Rendered: 5, Skipped: total - 5}, st)
		require.Contains(t, read(t, out.MemOutput, "posts/gamma/index.html"), "Edited.")
	})

	t.Run("a draft edit changes nothing while drafts are off", func(t *testing.T) {
		src["content/posts/wip.md"] = file("---\ntitle: WIP\ndate: 2026-04-01\ndraft: true\n---\nstill hidden\n")
		require.Equal(t, Stats{Skipped: total}, rebuild())
	})

	t.Run("changed template: every page re-renders, static files do not", func(t *testing.T) {
		src["templates/tag.html"] = file(`{{define "content"}}TAG {{.Tag}}{{template "postlist" .Posts}}{{end}}`)
		st := rebuild()
		require.Equal(t, Stats{Rendered: total - 2, Skipped: 2}, st)
		require.NotContains(t, out.take(), "robots.txt")
		require.Contains(t, read(t, out.MemOutput, "tags/go/index.html"), "TAG go")
	})

	t.Run("changed config: every page re-renders", func(t *testing.T) {
		src["sitegen.yaml"] = file("title: Renamed\nbaseURL: https://example.com\n")
		require.Equal(t, Stats{Rendered: total - 2, Skipped: 2}, rebuild())
		out.take()
	})

	t.Run("deleted post: its page goes away and listings update", func(t *testing.T) {
		delete(src, "content/posts/beta.md")
		st := rebuild()
		require.Equal(t, 1, st.Removed)
		require.Equal(t, []string{"posts/beta/index.html"}, out.gone)
		_, ok := out.Get("posts/beta/index.html")
		require.False(t, ok)
		require.Equal(t, []string{"feed.xml", "index.html", "sitemap.xml", "tags/go/index.html"}, out.take())
		require.NotContains(t, read(t, out.MemOutput, "index.html"), "Beta")
	})

	t.Run("a tag that loses its last post loses its page", func(t *testing.T) {
		src["content/posts/gamma.md"] = post("Gamma", "2026-03-05", "go")
		require.Equal(t, 0, rebuild().Removed)
		require.Contains(t, out.Paths(), "tags/web/index.html", "alpha still has the web tag")

		src["content/posts/alpha.md"] = post("Alpha", "2026-01-10", "go")
		require.Equal(t, 1, rebuild().Removed)
		require.NotContains(t, out.Paths(), "tags/web/index.html")
	})
}

func TestFailedWriteIsRetriedAndNothingElseRerenders(t *testing.T) {
	b := New()
	out := newSpy()
	out.failWriteOnce = "posts/beta/index.html"

	st, err := b.Build(t.Context(), baseSite(), out)
	require.ErrorContains(t, err, "disk full")
	require.ErrorContains(t, err, "posts/beta/index.html")
	require.Equal(t, 10, st.Rendered)

	st, err = b.Build(t.Context(), baseSite(), out)
	require.NoError(t, err)
	require.Equal(t, Stats{Rendered: 1, Skipped: 10}, st)
}

func TestWorkerCountDoesNotChangeOutput(t *testing.T) {
	src := baseSite()
	for i := range 40 {
		src[fmt.Sprintf("content/posts/p%02d.md", i)] = post(fmt.Sprintf("Post %d", i), fmt.Sprintf("2026-02-%02d", i%28+1), "go", fmt.Sprintf("t%d", i%5))
	}
	serial, _ := build(t, src, WithWorkers(1))
	parallel, _ := build(t, src, WithWorkers(8))

	require.Equal(t, serial.Paths(), parallel.Paths())
	for _, p := range serial.Paths() {
		require.Equal(t, read(t, serial, p), read(t, parallel, p), p)
	}
}

func TestBuildStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	out := NewMemOutput()
	_, err := New().Build(ctx, baseSite(), out)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, out.Paths())
}

func TestDirOutput(t *testing.T) {
	out := DirOutput(t.TempDir())
	require.NoError(t, out.Write("a/b/index.html", []byte("x")))
	require.NoError(t, out.Write("a/other.txt", []byte("y")))

	data, err := os.ReadFile(filepath.Join(string(out), "a", "b", "index.html"))
	require.NoError(t, err)
	require.Equal(t, "x", string(data))

	require.NoError(t, out.Remove("a/b/index.html"))
	require.NoDirExists(t, filepath.Join(string(out), "a", "b"), "emptied directory is pruned")
	require.DirExists(t, filepath.Join(string(out), "a"), "non-empty directory stays")
}
