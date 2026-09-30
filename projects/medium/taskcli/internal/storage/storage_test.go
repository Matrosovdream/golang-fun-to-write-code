package storage_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"taskcli/internal/storage"
	"taskcli/internal/task"
)

// One conformance suite runs against every backend: if a new implementation
// passes, it behaves like the others.
func TestSQLite(t *testing.T) {
	testRepository(t, func(t *testing.T) storage.Repository {
		r, err := storage.NewSQLite(filepath.Join(t.TempDir(), "tasks.db"))
		require.NoError(t, err)
		t.Cleanup(func() { r.Close() })
		return r
	})
}

func TestJSON(t *testing.T) {
	testRepository(t, func(t *testing.T) storage.Repository {
		r, err := storage.NewJSON(filepath.Join(t.TempDir(), "tasks.json"))
		require.NoError(t, err)
		t.Cleanup(func() { r.Close() })
		return r
	})
}

func newTask(title string, prio int) *task.Task {
	return &task.Task{
		Title:     title,
		Priority:  prio,
		Status:    task.StatusOpen,
		CreatedAt: time.Now().Truncate(time.Second),
	}
}

func testRepository(t *testing.T, newRepo func(t *testing.T) storage.Repository) {
	ctx := context.Background()

	t.Run("add assigns ids and get round-trips", func(t *testing.T) {
		repo := newRepo(t)

		a, b := newTask("first", 1), newTask("second", 2)
		require.NoError(t, repo.Add(ctx, a))
		require.NoError(t, repo.Add(ctx, b))
		require.NotZero(t, a.ID)
		require.NotEqual(t, a.ID, b.ID)

		got, err := repo.Get(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, "first", got.Title)
		require.Equal(t, task.StatusOpen, got.Status)
		require.Nil(t, got.DoneAt)
	})

	t.Run("get missing returns ErrNotFound", func(t *testing.T) {
		repo := newRepo(t)

		_, err := repo.Get(ctx, 42)
		require.ErrorIs(t, err, storage.ErrNotFound)
	})

	t.Run("list filters and limits", func(t *testing.T) {
		repo := newRepo(t)

		low, high, other := newTask("low", 3), newTask("high", 1), newTask("norm", 2)
		for _, tk := range []*task.Task{low, high, other} {
			require.NoError(t, repo.Add(ctx, tk))
		}
		require.NoError(t, repo.MarkDone(ctx, other.ID))

		tests := []struct {
			name       string
			opts       []storage.ListOption
			wantTitles []string
		}{
			{"all, priority order", nil, []string{"high", "norm", "low"}},
			{"open only", []storage.ListOption{storage.WithStatus(task.StatusOpen)}, []string{"high", "low"}},
			{"done only", []storage.ListOption{storage.WithStatus(task.StatusDone)}, []string{"norm"}},
			{"limit 1", []storage.ListOption{storage.WithLimit(1)}, []string{"high"}},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				got, err := repo.List(ctx, tc.opts...)
				require.NoError(t, err)
				var titles []string
				for _, tk := range got {
					titles = append(titles, tk.Title)
				}
				require.Equal(t, tc.wantTitles, titles)
			})
		}
	})

	t.Run("mark done sets status and timestamp", func(t *testing.T) {
		repo := newRepo(t)

		tk := newTask("finish me", 2)
		require.NoError(t, repo.Add(ctx, tk))
		require.NoError(t, repo.MarkDone(ctx, tk.ID))

		got, err := repo.Get(ctx, tk.ID)
		require.NoError(t, err)
		require.Equal(t, task.StatusDone, got.Status)
		require.NotNil(t, got.DoneAt)

		require.ErrorIs(t, repo.MarkDone(ctx, 999), storage.ErrNotFound)
	})

	t.Run("delete removes the task", func(t *testing.T) {
		repo := newRepo(t)

		tk := newTask("remove me", 2)
		require.NoError(t, repo.Add(ctx, tk))
		require.NoError(t, repo.Delete(ctx, tk.ID))

		_, err := repo.Get(ctx, tk.ID)
		require.ErrorIs(t, err, storage.ErrNotFound)
		require.ErrorIs(t, repo.Delete(ctx, tk.ID), storage.ErrNotFound)
	})
}
