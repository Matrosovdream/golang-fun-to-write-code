package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"

	"gobank/internal/domain"
)

type AccountRepo struct{ db *DB }

func NewAccountRepo(db *DB) *AccountRepo { return &AccountRepo{db: db} }

func (r *AccountRepo) Create(ctx context.Context, userID int64, currency string) (domain.Account, error) {
	var a domain.Account
	err := sqlx.GetContext(ctx, r.db.q(ctx), &a,
		`INSERT INTO accounts (user_id, currency) VALUES ($1, $2)
		 RETURNING id, user_id, currency, balance, created_at`,
		userID, currency)
	if err != nil {
		return domain.Account{}, fmt.Errorf("insert account: %w", err)
	}
	return a, nil
}

func (r *AccountRepo) ByID(ctx context.Context, id int64) (domain.Account, error) {
	return r.get(ctx, id, "")
}

// ByIDForUpdate blocks other transactions touching this row until ours
// commits — the foundation of a correct concurrent transfer.
func (r *AccountRepo) ByIDForUpdate(ctx context.Context, id int64) (domain.Account, error) {
	return r.get(ctx, id, " FOR UPDATE")
}

func (r *AccountRepo) get(ctx context.Context, id int64, suffix string) (domain.Account, error) {
	var a domain.Account
	err := sqlx.GetContext(ctx, r.db.q(ctx), &a,
		`SELECT id, user_id, currency, balance, created_at FROM accounts WHERE id = $1`+suffix, id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Account{}, domain.ErrAccountNotFound
	}
	if err != nil {
		return domain.Account{}, fmt.Errorf("select account: %w", err)
	}
	return a, nil
}

func (r *AccountRepo) ByUser(ctx context.Context, userID int64) ([]domain.Account, error) {
	var accounts []domain.Account
	err := sqlx.SelectContext(ctx, r.db.q(ctx), &accounts,
		`SELECT id, user_id, currency, balance, created_at
		 FROM accounts WHERE user_id = $1 ORDER BY id`, userID)
	if err != nil {
		return nil, fmt.Errorf("select accounts: %w", err)
	}
	return accounts, nil
}

func (r *AccountRepo) AddBalance(ctx context.Context, id, delta int64) error {
	res, err := r.db.q(ctx).ExecContext(ctx,
		`UPDATE accounts SET balance = balance + $1 WHERE id = $2`, delta, id)
	if err != nil {
		return fmt.Errorf("update balance: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.ErrAccountNotFound
	}
	return nil
}
