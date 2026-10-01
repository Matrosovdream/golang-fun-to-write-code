package main

import (
	"html/template"
	"io/fs"
	"net/http"
	"sort"
	"sync"
)

// listingTmpl is html/template, not text/template: every {{.}} is escaped
// for its context. A file named <script>evil</script> renders as text —
// the test proves it.
var listingTmpl = template.Must(template.New("listing").Parse(`<!doctype html>
<meta charset="utf-8">
<title>pageserve</title>
<h1>Files</h1>
<ul>
{{range .Entries}}  <li><a href="/files/{{.Name}}">{{.Name}}</a>{{if .IsDir}}/{{end}} — {{.Size}} bytes</li>
{{end}}</ul>
<p><a href="/stats">request stats</a></p>
`))

var statsTmpl = template.Must(template.New("stats").Parse(`<!doctype html>
<meta charset="utf-8">
<title>stats</title>
<h1>Requests</h1>
<table border="1" cellpadding="4">
<tr><th>path</th><th>hits</th></tr>
{{range .}}<tr><td>{{.Path}}</td><td>{{.Hits}}</td></tr>
{{end}}</table>
`))

type site struct {
	fsys fs.FS

	// Every request handler may run on its own goroutine — the server
	// decides, not you. Shared state therefore needs the mutex from the
	// very first counter.
	mu   sync.Mutex
	hits map[string]int
}

func newSite(fsys fs.FS) http.Handler {
	s := &site{fsys: fsys, hits: make(map[string]int)}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.listing) // {$} = exactly "/", not a catch-all
	mux.Handle("GET /files/", http.StripPrefix("/files/", http.FileServerFS(fsys)))
	mux.HandleFunc("GET /stats", s.stats)

	return s.countRequests(mux)
}

// countRequests is middleware: same shape as shortlink's, minimum version.
func (s *site) countRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

type entryView struct {
	Name  string
	IsDir bool
	Size  int64
}

func (s *site) listing(w http.ResponseWriter, r *http.Request) {
	entries, err := fs.ReadDir(s.fsys, ".")
	if err != nil {
		http.Error(w, "cannot read directory", http.StatusInternalServerError)
		return
	}

	var views []entryView
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		views = append(views, entryView{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size()})
	}
	if err := listingTmpl.Execute(w, map[string]any{"Entries": views}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type statView struct {
	Path string
	Hits int
}

func (s *site) stats(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	views := make([]statView, 0, len(s.hits))
	for path, hits := range s.hits {
		views = append(views, statView{path, hits})
	}
	s.mu.Unlock() // copy under lock, render after — templates can be slow

	sort.Slice(views, func(i, j int) bool { return views[i].Path < views[j].Path })
	if err := statsTmpl.Execute(w, views); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
