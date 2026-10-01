package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"time"
)

type Todo struct {
	ID        int        `json:"id"`
	Title     string     `json:"title"`
	CreatedAt time.Time  `json:"created_at"`
	DoneAt    *time.Time `json:"done_at,omitempty"` // nil while open
}

func (t Todo) Done() bool { return t.DoneAt != nil }

// Store is the whole persistence layer: a struct in memory, a JSON file on
// disk. Load-mutate-save is plenty at this scale — taskcli (medium) shows
// what replaces it when it stops being plenty.
type Store struct {
	path   string
	NextID int    `json:"next_id"`
	Todos  []Todo `json:"todos"`
}

func Load(path string) (*Store, error) {
	s := &Store{path: path, NextID: 1}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil // first run: empty store
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, s); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", path, err)
	}
	return s, nil
}

func (s *Store) Save() error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o644)
}

func (s *Store) Add(title string) Todo {
	t := Todo{ID: s.NextID, Title: title, CreatedAt: time.Now()}
	s.NextID++
	s.Todos = append(s.Todos, t)
	return t
}

func (s *Store) MarkDone(id int) (Todo, error) {
	// slices.IndexFunc: the stdlib generic helpers replace hand-rolled loops.
	i := slices.IndexFunc(s.Todos, func(t Todo) bool { return t.ID == id })
	if i < 0 {
		return Todo{}, fmt.Errorf("no todo with id %d", id)
	}
	now := time.Now()
	s.Todos[i].DoneAt = &now
	return s.Todos[i], nil
}

func (s *Store) Delete(id int) error {
	before := len(s.Todos)
	s.Todos = slices.DeleteFunc(s.Todos, func(t Todo) bool { return t.ID == id })
	if len(s.Todos) == before {
		return fmt.Errorf("no todo with id %d", id)
	}
	return nil
}
