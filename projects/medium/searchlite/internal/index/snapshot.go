package index

import (
	"encoding/gob"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type snapshot struct {
	Docs     []Doc
	Postings map[string][]Posting
}

// Save writes the whole index to path, atomically: readers of path see either
// the previous snapshot or this one, never a mixture.
func (ix *Index) Save(path string) error {
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return writeAtomic(path, func(w io.Writer) error {
		return gob.NewEncoder(w).Encode(snapshot{ix.docs, ix.postings})
	})
}

// Load reads a snapshot written by Save. A missing file is reported with an
// error satisfying errors.Is(err, fs.ErrNotExist).
func Load(path string, opts ...Option) (*Index, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var s snapshot
	if err := gob.NewDecoder(f).Decode(&s); err != nil {
		return nil, fmt.Errorf("decode snapshot %s: %w", path, err)
	}
	ix := New(opts...)
	ix.docs = s.Docs
	if s.Postings != nil { // gob drops empty maps; keep ours writable
		ix.postings = s.Postings
	}
	for i, d := range s.Docs {
		ix.byName[d.Name] = i
		ix.tokens += d.Len
	}
	return ix, nil
}

// writeAtomic replaces path with what write produces, or leaves path alone.
// The bytes go to a temp file next to path (rename is only atomic within one
// filesystem) and are fsynced before the rename, so a crash can never leave a
// half-written file under the real name. The directory is fsynced afterwards
// so the rename itself survives power loss.
func writeAtomic(path string, write func(io.Writer) error) (err error) {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
		}
	}()
	if err = write(f); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
