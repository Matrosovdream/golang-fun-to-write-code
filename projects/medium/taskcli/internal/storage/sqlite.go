package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" driver

	"taskcli/internal/task"
)

type SQLite struct {
	db *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS tasks (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	title      TEXT    NOT NULL,
	priority   INTEGER NOT NULL DEFAULT 2,
	status     TEXT    NOT NULL DEFAULT 'open',
	created_at TIMESTAMP NOT NULL,
	done_at    TIMESTAMP
);`

func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Add(ctx context.Context, t *task.Task) error {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO tasks (title, priority, status, created_at) VALUES (?, ?, ?, ?)`,
		t.Title, t.Priority, t.Status, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	t.ID, err = res.LastInsertId()
	return err
}

func (s *SQLite) Get(ctx context.Context, id int64) (task.Task, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, title, priority, status, created_at, done_at FROM tasks WHERE id = ?`, id)
	t, err := scanTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return task.Task{}, fmt.Errorf("id %d: %w", id, ErrNotFound)
	}
	return t, err
}

func (s *SQLite) List(ctx context.Context, opts ...ListOption) ([]task.Task, error) {
	var f Filter
	for _, opt := range opts {
		opt(&f)
	}

	query := `SELECT id, title, priority, status, created_at, done_at FROM tasks`
	var args []any
	if f.Status != "" {
		query += ` WHERE status = ?`
		args = append(args, f.Status)
	}
	query += ` ORDER BY priority, id`
	if f.Limit > 0 {
		query += ` LIMIT ?`
		args = append(args, f.Limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []task.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *SQLite) MarkDone(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE tasks SET status = ?, done_at = ? WHERE id = ?`,
		task.StatusDone, time.Now(), id)
	if err != nil {
		return fmt.Errorf("mark done: %w", err)
	}
	return checkAffected(res, id)
}

func (s *SQLite) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	return checkAffected(res, id)
}

func (s *SQLite) Close() error { return s.db.Close() }

// scanner covers both *sql.Row and *sql.Rows, so one scan helper serves
// Get and List.
type scanner interface {
	Scan(dest ...any) error
}

func scanTask(row scanner) (task.Task, error) {
	var (
		t      task.Task
		doneAt sql.NullTime
	)
	err := row.Scan(&t.ID, &t.Title, &t.Priority, &t.Status, &t.CreatedAt, &doneAt)
	if err != nil {
		return task.Task{}, err
	}
	if doneAt.Valid {
		t.DoneAt = &doneAt.Time
	}
	return t, nil
}

func checkAffected(res sql.Result, id int64) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("id %d: %w", id, ErrNotFound)
	}
	return nil
}
