// Package watch turns filesystem notifications into rebuild signals.
package watch

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/fsnotify/fsnotify"
)

// Ignored reports whether a path is editor or OS noise: dotfiles (which covers
// .#lock files and .name.swp), backup files (name~) and swap/temp files.
func Ignored(name string) bool {
	base := filepath.Base(name)
	return strings.HasPrefix(base, ".") || strings.HasSuffix(base, "~") ||
		strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swx") || strings.HasSuffix(base, ".tmp")
}

// Dir reports the path of every relevant change under root until ctx is done.
//
// fsnotify is not recursive: each directory needs its own watch, and
// directories created later (mkdir, git checkout, unzip) must be added as they
// appear. The watch is added before the event is forwarded, so a consumer that
// reacts to the new directory's event can rely on its contents being watched.
func Dir(ctx context.Context, root string) (<-chan string, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := addTree(w, root); err != nil {
		w.Close()
		return nil, err
	}
	out := make(chan string)
	go func() {
		defer close(out)
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.Errors: // must be drained; the next event triggers a rebuild anyway
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op == fsnotify.Chmod || Ignored(ev.Name) {
					continue
				}
				if ev.Has(fsnotify.Create) {
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = addTree(w, ev.Name) // may have vanished again already
					}
				}
				select {
				case out <- ev.Name:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, nil
}

func addTree(w *fsnotify.Watcher, root string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return err
		}
		if p != root && Ignored(p) {
			return filepath.SkipDir
		}
		return w.Add(p)
	})
}
