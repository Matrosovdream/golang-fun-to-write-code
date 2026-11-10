package watch

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIgnored(t *testing.T) {
	for name, want := range map[string]bool{
		"content/posts/a.md":         false,
		"templates/post.html":        false,
		"a.md~":                      true, // editor backup
		"content/.a.md.swp":          true, // vim swap file
		"content/.a.md.swx":          true,
		"content/.#a.md":             true, // emacs lock
		".DS_Store":                  true,
		"content/a.md.tmp":           true,
		"sitegen.yaml":               false,
		"static/images/logo.swp.png": false,
	} {
		require.Equal(t, want, Ignored(name), name)
	}
}

// waitFor drains events until one for path arrives and returns everything it saw.
func waitFor(t *testing.T, events <-chan string, path string) []string {
	t.Helper()
	var seen []string
	timeout := time.After(10 * time.Second)
	for {
		select {
		case p, ok := <-events:
			require.True(t, ok, "watcher stopped; saw %v", seen)
			seen = append(seen, p)
			if p == path {
				return seen
			}
		case <-timeout:
			t.Fatalf("no event for %s; saw %v", path, seen)
		}
	}
}

func TestDirReportsChangesAndSkipsEditorNoise(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir()) // macOS: /var -> /private/var
	require.NoError(t, err)
	events, err := Dir(t.Context(), root)
	require.NoError(t, err)

	// Events arrive in the order they happen, so everything written before
	// post.md is in `seen` by the time post.md shows up.
	for _, name := range []string{".post.md.swp", "post.md~", ".#post.md"} {
		require.NoError(t, os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644))
	}
	post := filepath.Join(root, "post.md")
	require.NoError(t, os.WriteFile(post, []byte("hello"), 0o644))

	for _, p := range waitFor(t, events, post) {
		require.False(t, Ignored(p), "forwarded noise: %s", p)
	}
}

func TestDirWatchesDirectoriesCreatedLater(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	events, err := Dir(t.Context(), root)
	require.NoError(t, err)

	// One level at a time: the event for a new directory is forwarded only
	// after its watch is in place, so it is safe to write inside it right after.
	a := filepath.Join(root, "a")
	require.NoError(t, os.Mkdir(a, 0o755))
	waitFor(t, events, a)
	b := filepath.Join(a, "b")
	require.NoError(t, os.Mkdir(b, 0o755))
	waitFor(t, events, b)

	file := filepath.Join(b, "deep.md")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o644))
	waitFor(t, events, file)
}

func TestDirClosesWhenContextEnds(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	events, err := Dir(ctx, t.TempDir())
	require.NoError(t, err)
	cancel()
	for range events { // closed once the goroutine exits
	}
}

func TestDirMissingRoot(t *testing.T) {
	_, err := Dir(t.Context(), filepath.Join(t.TempDir(), "nope"))
	require.Error(t, err)
}
