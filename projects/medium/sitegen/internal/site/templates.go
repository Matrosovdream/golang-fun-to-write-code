package site

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"maps"
	"path"
	"slices"
	"time"
)

//go:embed theme
var themeRoot embed.FS

// theme is the embedded default: templates/*.html and static/**.
var theme = func() fs.FS {
	sub, err := fs.Sub(themeRoot, "theme")
	if err != nil {
		panic(err)
	}
	return sub
}()

// layouts are the entry templates. Each one only {{define}}s the blocks that
// base.html declares with {{block}}; base.html and any partials are shared.
var layouts = []string{"post.html", "page.html", "list.html", "tag.html"}

var funcs = template.FuncMap{
	"date":   func(t time.Time) string { return t.Format("2 Jan 2006") },
	"tagURL": tagURL,
}

func tagURL(tag string) string { return "/tags/" + tag + "/" }

// loadTemplates merges the theme's templates with the site's own templates/
// directory (same file name wins) and returns one parsed set per layout.
//
// One set per layout because a namespace holds a single "content" definition:
// parsing post.html and tag.html together would make the last one win
// everywhere. The key hashes every template byte.
func loadTemplates(src fs.FS) (map[string]*template.Template, string, error) {
	files := map[string]string{}
	for _, fsys := range []fs.FS{theme, src} {
		names, _ := fs.Glob(fsys, "templates/*.html") // the only error is a bad pattern
		for _, name := range names {
			data, err := fs.ReadFile(fsys, name)
			if err != nil {
				return nil, "", err
			}
			files[path.Base(name)] = string(data)
		}
	}

	names := slices.Sorted(maps.Keys(files))
	var key []string
	for _, name := range names {
		key = append(key, name, files[name])
	}
	sets := map[string]*template.Template{}
	for _, layout := range layouts {
		set := template.New("").Funcs(funcs)
		for _, name := range names {
			if name != layout && slices.Contains(layouts, name) {
				continue
			}
			if _, err := set.New(name).Parse(files[name]); err != nil {
				return nil, "", fmt.Errorf("templates/%s: %w", name, err)
			}
		}
		sets[layout] = set
	}
	return sets, hashOf(key...), nil
}
