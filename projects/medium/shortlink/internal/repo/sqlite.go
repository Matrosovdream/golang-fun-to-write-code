package repo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"shortlink/internal/link"
)

type SQLite struct {
	db *sql.DB
}

func NewSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS links (
		code       TEXT PRIMARY KEY,
		url        TEXT NOT NULL,
		hits       INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP NOT NULL
	)`)
	if err != nil {
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &SQLite{db: db}, nil
}

func (s *SQLite) Save(ctx context.Context, l link.Link) error {
	// INSERT OR IGNORE + RowsAffected detects a duplicate code in one
	// statement — a SELECT-then-INSERT pair would race between two requests.
	res, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO links (code, url, hits, created_at) VALUES (?, ?, 0, ?)`,
		l.Code, l.URL, l.CreatedAt)
	if err != nil {
		return fmt.Errorf("save link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrCodeTaken
	}
	return nil
}

func (s *SQLite) Get(ctx context.Context, code string) (link.Link, error) {
	var l link.Link
	err := s.db.QueryRowContext(ctx,
		`SELECT code, url, hits, created_at FROM links WHERE code = ?`, code).
		Scan(&l.Code, &l.URL, &l.Hits, &l.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return link.Link{}, ErrNotFound
	}
	return l, err
}

func (s *SQLite) Touch(ctx context.Context, code string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE links SET hits = hits + 1 WHERE code = ?`, code)
	if err != nil {
		return fmt.Errorf("touch link: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLite) Close() error { return s.db.Close() }
