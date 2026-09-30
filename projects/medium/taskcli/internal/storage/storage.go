package storage

import (
	"context"
	"errors"
	"fmt"

	"taskcli/internal/task"
)

// ErrNotFound is a sentinel error: callers match it with errors.Is,
// no matter which backend produced it.
var ErrNotFound = errors.New("task not found")

type Filter struct {
	Status task.Status // empty = any
	Limit  int         // 0 = no limit
}

// ListOption is the functional-options pattern: callers compose only the
// filters they need, and the List signature never has to change.
type ListOption func(*Filter)

func WithStatus(s task.Status) ListOption { return func(f *Filter) { f.Status = s } }
func WithLimit(n int) ListOption          { return func(f *Filter) { f.Limit = n } }

type Repository interface {
	Add(ctx context.Context, t *task.Task) error
	Get(ctx context.Context, id int64) (task.Task, error)
	List(ctx context.Context, opts ...ListOption) ([]task.Task, error)
	MarkDone(ctx context.Context, id int64) error
	Delete(ctx context.Context, id int64) error
	Close() error
}

type Config struct {
	Backend string // "sqlite" or "json"
	Path    string
}

// New is a factory: the rest of the app depends only on the Repository
// interface and never knows which implementation it got.
func New(cfg Config) (Repository, error) {
	switch cfg.Backend {
	case "sqlite":
		return NewSQLite(cfg.Path)
	case "json":
		return NewJSON(cfg.Path)
	default:
		return nil, fmt.Errorf("unknown storage backend %q", cfg.Backend)
	}
}
