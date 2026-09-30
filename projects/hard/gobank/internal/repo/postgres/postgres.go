// Package postgres implements the repo interfaces on PostgreSQL via sqlx
// with the pgx driver.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5" // migrate's pgx driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jmoiron/sqlx"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	*sqlx.DB
}

func Connect(ctx context.Context, dsn string) (*DB, error) {
	db, err := sqlx.ConnectContext(ctx, "pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	// Pool sizing is part of connecting, not an afterthought.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	return &DB{DB: db}, nil
}

// Migrate applies embedded migrations — the binary carries its own schema,
// so "clone and run" needs no external tooling.
func Migrate(dsn string) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	// migrate picks its DB driver by URL scheme; its pgx/v5 driver
	// registered itself as "pgx5", so the standard postgres:// DSN needs
	// its scheme swapped just for this call.
	migrateDSN := strings.Replace(dsn, "postgres://", "pgx5://", 1)
	m, err := migrate.NewWithSourceInstance("iofs", src, migrateDSN)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}
	srcErr, dbErr := m.Close()
	return errors.Join(srcErr, dbErr)
}

// --- unit of work ---

type txKey struct{}

func (d *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if _, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return fn(ctx) // already inside a transaction — join it
	}
	tx, err := d.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	// The tx travels down via context; repos transparently pick it up in q().
	if err := fn(context.WithValue(ctx, txKey{}, tx)); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
		}
		return err
	}
	return tx.Commit()
}

// q returns the current transaction if one is in the context, otherwise the
// pool. Every query in this package goes through it.
func (d *DB) q(ctx context.Context) sqlx.ExtContext {
	if tx, ok := ctx.Value(txKey{}).(*sqlx.Tx); ok {
		return tx
	}
	return d.DB
}
