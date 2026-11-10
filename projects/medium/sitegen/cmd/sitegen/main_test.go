package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildTheExampleSite(t *testing.T) {
	out := filepath.Join(t.TempDir(), "public")
	var stdout bytes.Buffer
	// The directory comes before the flag, as in the README.
	require.NoError(t, run(t.Context(), []string{"build", "../../example", "-o", out}, &stdout))
	require.Contains(t, stdout.String(), "files written")

	var files []string
	require.NoError(t, fs.WalkDir(os.DirFS(out), ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
			data, err := os.ReadFile(filepath.Join(out, p))
			require.NoError(t, err)
			require.NotContains(t, string(data), "__reload", "%s: the live-reload script belongs to serve mode only", p)
		}
		return err
	}))
	require.Contains(t, files, "posts/hello-sitegen/index.html")
	require.Contains(t, files, "robots.txt")
	require.NotContains(t, files, "posts/unfinished-thoughts/index.html", "draft")

	about, err := os.ReadFile(filepath.Join(out, "about", "index.html"))
	require.NoError(t, err)
	require.Contains(t, string(about), `<a href="/about/">About</a>`, "the example overrides base.html to add this link")
}

func TestBuildTheExampleWithDrafts(t *testing.T) {
	out := t.TempDir()
	require.NoError(t, run(t.Context(), []string{"build", "-drafts", "-o", out, "../../example"}, &bytes.Buffer{}))
	require.FileExists(t, filepath.Join(out, "posts", "unfinished-thoughts", "index.html"))
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{nil, {"nope"}, {"build"}, {"serve"}, {"build", "a", "b"}} {
		err := run(t.Context(), args, &bytes.Buffer{})
		require.Error(t, err, args)
		require.True(t, strings.HasPrefix(err.Error(), "usage:"), args)
	}
}
