package site

import (
	"os"
	"path/filepath"
	"sync"
)

// Output is where built files go. Paths are slash-separated and relative to
// the site root. Implementations must be safe for concurrent use: the worker
// pool writes pages in parallel.
type Output interface {
	Write(path string, data []byte) error
	Remove(path string) error
}

// DirOutput writes into a directory on disk.
type DirOutput string

func (d DirOutput) Write(path string, data []byte) error {
	full := filepath.Join(string(d), filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, data, 0o644)
}

// Remove also drops the parent directory if that emptied it (os.Remove
// refuses to delete a non-empty directory, which is exactly the check we want).
func (d DirOutput) Remove(path string) error {
	full := filepath.Join(string(d), filepath.FromSlash(path))
	err := os.Remove(full)
	if err == nil {
		_ = os.Remove(filepath.Dir(full))
	}
	return err
}

// MemOutput keeps the built site in memory: the dev server serves from it and
// tests assert on it.
type MemOutput struct {
	mu    sync.RWMutex
	files map[string][]byte
}

func NewMemOutput() *MemOutput { return &MemOutput{files: map[string][]byte{}} }

func (m *MemOutput) Write(path string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path] = data
	return nil
}

func (m *MemOutput) Remove(path string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, path)
	return nil
}

func (m *MemOutput) Get(path string) ([]byte, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	data, ok := m.files[path]
	return data, ok
}
