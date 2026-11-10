// Package sqlite opens the per-service database and wraps transactions.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Open avoids SQLITE_BUSY three ways: WAL (readers never block the writer),
// busy_timeout (a second writer waits instead of failing) and
// _txlock=immediate (a transaction takes the write lock at BEGIN, so it can
// never fail halfway while upgrading from a read lock).
func Open(path string) (*sql.DB, error) {
	dsn := "file:" + path + "?_txlock=immediate&_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	return db, nil
}

// InTx runs fn in one transaction: commit if it returns nil, roll back
// otherwise. Keep network calls out of fn — the write lock is held meanwhile.
func InTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}
