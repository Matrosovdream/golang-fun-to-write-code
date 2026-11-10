// Package site turns a Markdown tree (an fs.FS) into a static site (an Output).
//
// A build is a pipeline: discover and parse sources -> plan one unit per output
// file -> render only the units whose key changed -> write -> prune outputs that
// no longer exist. A unit's key is the sha256 of everything that can change its
// bytes: the config, the whole template set, and the identity and source of
// every page it shows. If the key matches the previous build the unit is
// skipped, so editing one post re-renders that post and the listings that
// mention it, and nothing else.
package site

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"path"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
)

// Raw HTML in Markdown is dropped (goldmark's default), so the only trusted
// HTML in a page is what goldmark generates itself.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
)

type Stats struct{ Rendered, Skipped, Removed int }

// Builder remembers, per output path, the key of the content last written, so
// a second Build into the same Output skips unchanged files. Not safe for
// concurrent Build calls.
type Builder struct {
	drafts  bool
	workers int
	cache   map[string]string
}

type Option func(*Builder)

func WithDrafts(on bool) Option { return func(b *Builder) { b.drafts = on } }
func WithWorkers(n int) Option  { return func(b *Builder) { b.workers = n } }

func New(opts ...Option) *Builder {
	b := &Builder{workers: runtime.GOMAXPROCS(0), cache: map[string]string{}}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// unit is one output file: render produces its bytes, key says whether the
// previous build's bytes are still valid.
type unit struct {
	path   string
	key    string
	render func() ([]byte, error)
}

// Build reads the site from src and writes it to out, removing outputs of
// earlier builds that no longer exist.
func (b *Builder) Build(ctx context.Context, src fs.FS, out Output) (Stats, error) {
	cfg, err := loadConfig(src)
	if err != nil {
		return Stats{}, err
	}
	tpls, tplKey, err := loadTemplates(src)
	if err != nil {
		return Stats{}, err
	}
	pages, err := b.discover(src)
	if err != nil {
		return Stats{}, err
	}
	static := map[string][]byte{}
	for _, fsys := range []fs.FS{theme, src} { // the site's files win
		if err := readStatic(fsys, static); err != nil {
			return Stats{}, err
		}
	}

	s := newSiteData(cfg, tpls, hashOf(fmt.Sprintf("%+v", cfg), tplKey), pages)
	units := s.units()
	for _, name := range slices.Sorted(maps.Keys(static)) {
		data := static[name]
		units = append(units, unit{name, hashOf(string(data)), func() ([]byte, error) { return data, nil }})
	}
	return b.execute(ctx, units, out)
}

func (b *Builder) discover(src fs.FS) ([]*Page, error) {
	var pages []*Page
	var errs []error
	err := fs.WalkDir(src, "content", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path.Ext(name) != ".md" || strings.HasPrefix(d.Name(), ".") {
			return err
		}
		data, err := fs.ReadFile(src, name)
		if err != nil {
			return err
		}
		switch p, err := parsePage(name, data); {
		case err != nil:
			errs = append(errs, err) // keep walking: report every broken file at once
		case !p.Draft || b.drafts:
			pages = append(pages, p)
		}
		return nil
	})
	return pages, errors.Join(append(errs, err)...)
}

func readStatic(fsys fs.FS, dst map[string][]byte) error {
	err := fs.WalkDir(fsys, "static", func(name string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(fsys, name)
		dst[strings.TrimPrefix(name, "static/")] = data
		return err
	})
	if errors.Is(err, fs.ErrNotExist) { // no static/ directory
		return nil
	}
	return err
}

// execute renders the stale units on a worker pool, then prunes. Workers write
// distinct errs[i] slots, so results need no lock; the cache is only touched
// here, after wg.Wait.
func (b *Builder) execute(ctx context.Context, units []unit, out Output) (Stats, error) {
	var st Stats
	live := make(map[string]bool, len(units))
	var stale []int
	for i, u := range units {
		live[u.path] = true
		if b.cache[u.path] == u.key {
			st.Skipped++
		} else {
			stale = append(stale, i)
		}
	}

	errs := make([]error, len(units))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range max(1, min(b.workers, len(stale))) {
		wg.Go(func() {
			for i := range jobs {
				if ctx.Err() == nil {
					errs[i] = emit(units[i], out)
				}
			}
		})
	}
	for _, i := range stale {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return st, err
	}

	for _, i := range stale {
		if errs[i] == nil { // a failed unit keeps its old key and is retried next build
			b.cache[units[i].path] = units[i].key
			st.Rendered++
		}
	}
	for _, p := range slices.Sorted(maps.Keys(b.cache)) {
		if live[p] {
			continue
		}
		if err := out.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, err)
			continue
		}
		delete(b.cache, p)
		st.Removed++
	}
	return st, errors.Join(errs...)
}

func emit(u unit, out Output) error {
	data, err := u.render()
	if err != nil {
		return fmt.Errorf("render %s: %w", u.path, err)
	}
	if err := out.Write(u.path, data); err != nil {
		return fmt.Errorf("write %s: %w", u.path, err)
	}
	return nil
}

// siteData is the immutable snapshot one Build renders from; render closures
// read it concurrently.
type siteData struct {
	cfg   Config
	tpls  map[string]*template.Template
	env   string // key ingredients shared by every output
	pages []*Page
	posts []*Page // newest first
	tags  map[string][]*Page
}

type pageData struct {
	Site  Config
	Page  *Page
	Body  template.HTML
	Posts []*Page
	Tag   string
}

func newSiteData(cfg Config, tpls map[string]*template.Template, env string, pages []*Page) *siteData {
	s := &siteData{cfg: cfg, tpls: tpls, env: env, pages: pages, tags: map[string][]*Page{}}
	for _, p := range pages {
		if p.IsPost {
			s.posts = append(s.posts, p)
		}
	}
	slices.SortFunc(s.posts, func(a, b *Page) int { // newest first, URL breaks ties
		return cmp.Or(b.Date.Compare(a.Date), strings.Compare(a.URL, b.URL))
	})
	for _, p := range s.posts {
		for _, t := range p.Tags {
			s.tags[t] = append(s.tags[t], p)
		}
	}
	return s
}

// key hashes the shared ingredients plus the identity and source of every page
// an output depends on.
func (s *siteData) key(kind string, pages []*Page) string {
	parts := []string{s.env, kind}
	for _, p := range pages {
		parts = append(parts, p.URL, p.hash)
	}
	return hashOf(parts...)
}

func (s *siteData) units() []unit {
	var us []unit
	for _, p := range s.pages {
		us = append(us, unit{p.URL[1:] + "index.html", s.key("page", []*Page{p}), func() ([]byte, error) {
			return s.renderPage(p)
		}})
	}
	us = append(us, unit{"index.html", s.key("list", s.posts), func() ([]byte, error) {
		return s.exec("list.html", pageData{Posts: s.posts})
	}})
	for _, tag := range slices.Sorted(maps.Keys(s.tags)) {
		posts := s.tags[tag]
		us = append(us, unit{"tags/" + tag + "/index.html", s.key("tag:"+tag, posts), func() ([]byte, error) {
			return s.exec("tag.html", pageData{Tag: tag, Posts: posts})
		}})
	}
	feed := s.posts[:min(len(s.posts), feedLimit)]
	return append(us,
		unit{"feed.xml", s.key("feed", feed), func() ([]byte, error) { return s.rss(feed) }},
		unit{"sitemap.xml", s.key("sitemap", s.pages), s.sitemap},
	)
}

func (s *siteData) renderPage(p *Page) ([]byte, error) {
	var body bytes.Buffer
	if err := md.Convert([]byte(p.body), &body); err != nil {
		return nil, err
	}
	layout := "page.html"
	if p.IsPost {
		layout = "post.html"
	}
	return s.exec(layout, pageData{Page: p, Body: template.HTML(body.String())})
}

func (s *siteData) exec(layout string, d pageData) ([]byte, error) {
	d.Site = s.cfg
	var buf bytes.Buffer
	if err := s.tpls[layout].ExecuteTemplate(&buf, "base.html", d); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func hashOf(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		fmt.Fprintf(h, "%d:%s", len(p), p)
	}
	return fmt.Sprintf("%x", h.Sum(nil))
}
