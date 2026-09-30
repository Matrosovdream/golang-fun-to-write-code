package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"taskcli/internal/task"
)

// JSON keeps everything in one file. Fine for small data; the interesting
// parts are the atomic write and guarding in-memory state with a mutex.
type JSON struct {
	path string
	mu   sync.Mutex
	data jsonFile
}

type jsonFile struct {
	NextID int64       `json:"next_id"`
	Tasks  []task.Task `json:"tasks"`
}

func NewJSON(path string) (*JSON, error) {
	j := &JSON{path: path, data: jsonFile{NextID: 1}}

	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return j, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, &j.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return j, nil
}

func (j *JSON) Add(_ context.Context, t *task.Task) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	t.ID = j.data.NextID
	j.data.NextID++
	j.data.Tasks = append(j.data.Tasks, *t)
	return j.save()
}

func (j *JSON) Get(_ context.Context, id int64) (task.Task, error) {
	j.mu.Lock()
	defer j.mu.Unlock()

	i := j.index(id)
	if i < 0 {
		return task.Task{}, fmt.Errorf("id %d: %w", id, ErrNotFound)
	}
	return j.data.Tasks[i], nil
}

func (j *JSON) List(_ context.Context, opts ...ListOption) ([]task.Task, error) {
	var f Filter
	for _, opt := range opts {
		opt(&f)
	}

	j.mu.Lock()
	defer j.mu.Unlock()

	var out []task.Task
	for _, t := range j.data.Tasks {
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Priority != out[b].Priority {
			return out[a].Priority < out[b].Priority
		}
		return out[a].ID < out[b].ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (j *JSON) MarkDone(_ context.Context, id int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	i := j.index(id)
	if i < 0 {
		return fmt.Errorf("id %d: %w", id, ErrNotFound)
	}
	now := time.Now()
	j.data.Tasks[i].Status = task.StatusDone
	j.data.Tasks[i].DoneAt = &now
	return j.save()
}

func (j *JSON) Delete(_ context.Context, id int64) error {
	j.mu.Lock()
	defer j.mu.Unlock()

	i := j.index(id)
	if i < 0 {
		return fmt.Errorf("id %d: %w", id, ErrNotFound)
	}
	j.data.Tasks = append(j.data.Tasks[:i], j.data.Tasks[i+1:]...)
	return j.save()
}

func (j *JSON) Close() error { return nil }

// index must be called with the mutex held.
func (j *JSON) index(id int64) int {
	for i, t := range j.data.Tasks {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// save writes to a temp file and renames it over the target: the rename is
// atomic on POSIX, so a crash mid-write can never corrupt the data file.
func (j *JSON) save() error {
	raw, err := json.MarshalIndent(j.data, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(j.path), ".taskcli-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), j.path)
}
