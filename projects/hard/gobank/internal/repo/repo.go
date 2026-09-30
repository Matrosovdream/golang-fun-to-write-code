// Package repo defines the persistence interfaces the services depend on.
// The postgres subpackage implements them; tests may substitute fakes.
package repo

import (
	"context"

	"gobank/internal/domain"
)

type Users interface {
	Create(ctx context.Context, email, passwordHash string) (domain.User, error)
	ByEmail(ctx context.Context, email string) (domain.User, error)
}

type Accounts interface {
	Create(ctx context.Context, userID int64, currency string) (domain.Account, error)
	ByID(ctx context.Context, id int64) (domain.Account, error)
	// ByIDForUpdate takes a row lock (SELECT ... FOR UPDATE) — only valid
	// inside WithinTx.
	ByIDForUpdate(ctx context.Context, id int64) (domain.Account, error)
	ByUser(ctx context.Context, userID int64) ([]domain.Account, error)
	AddBalance(ctx context.Context, id, delta int64) error
}

type Transfers interface {
	Create(ctx context.Context, from *int64, to, amount int64) (domain.Transfer, error)
	History(ctx context.Context, accountID int64) ([]domain.Transfer, error)
}

// TxManager is the unit-of-work seam: the service composes several repo
// calls into one atomic operation without knowing anything about *sql.Tx.
type TxManager interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
