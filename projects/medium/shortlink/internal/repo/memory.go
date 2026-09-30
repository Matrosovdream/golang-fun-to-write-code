package repo

import (
	"context"
	"sync"

	"shortlink/internal/link"
)

// Memory is safe for concurrent use: reads take the shared lock, writes the
// exclusive one. RWMutex pays off here because redirects (reads) vastly
// outnumber shortens (writes).
type Memory struct {
	mu    sync.RWMutex
	links map[string]link.Link
}

func NewMemory() *Memory {
	return &Memory{links: make(map[string]link.Link)}
}

func (m *Memory) Save(_ context.Context, l link.Link) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.links[l.Code]; exists {
		return ErrCodeTaken
	}
	m.links[l.Code] = l
	return nil
}

func (m *Memory) Get(_ context.Context, code string) (link.Link, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	l, ok := m.links[code]
	if !ok {
		return link.Link{}, ErrNotFound
	}
	return l, nil
}

func (m *Memory) Touch(_ context.Context, code string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	l, ok := m.links[code]
	if !ok {
		return ErrNotFound
	}
	l.Hits++
	m.links[code] = l
	return nil
}
