package postgres

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"

	"gobank/internal/domain"
)

type TransferRepo struct{ db *DB }

func NewTransferRepo(db *DB) *TransferRepo { return &TransferRepo{db: db} }

func (r *TransferRepo) Create(ctx context.Context, from *int64, to, amount int64) (domain.Transfer, error) {
	var t domain.Transfer
	err := sqlx.GetContext(ctx, r.db.q(ctx), &t,
		`INSERT INTO transfers (from_account_id, to_account_id, amount) VALUES ($1, $2, $3)
		 RETURNING id, from_account_id, to_account_id, amount, created_at`,
		from, to, amount)
	if err != nil {
		return domain.Transfer{}, fmt.Errorf("insert transfer: %w", err)
	}
	return t, nil
}

func (r *TransferRepo) History(ctx context.Context, accountID int64) ([]domain.Transfer, error) {
	var transfers []domain.Transfer
	err := sqlx.SelectContext(ctx, r.db.q(ctx), &transfers,
		`SELECT id, from_account_id, to_account_id, amount, created_at
		 FROM transfers
		 WHERE from_account_id = $1 OR to_account_id = $1
		 ORDER BY id DESC LIMIT 100`, accountID)
	if err != nil {
		return nil, fmt.Errorf("select transfers: %w", err)
	}
	return transfers, nil
}
