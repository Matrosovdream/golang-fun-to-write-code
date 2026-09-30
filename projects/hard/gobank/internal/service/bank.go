package service

import (
	"context"
	"fmt"

	"gobank/internal/domain"
	"gobank/internal/repo"
)

type Bank struct {
	accounts  repo.Accounts
	transfers repo.Transfers
	tx        repo.TxManager
}

func NewBank(accounts repo.Accounts, transfers repo.Transfers, tx repo.TxManager) *Bank {
	return &Bank{accounts: accounts, transfers: transfers, tx: tx}
}

func (b *Bank) CreateAccount(ctx context.Context, userID int64, currency string) (domain.Account, error) {
	return b.accounts.Create(ctx, userID, currency)
}

func (b *Bank) MyAccounts(ctx context.Context, userID int64) ([]domain.Account, error) {
	return b.accounts.ByUser(ctx, userID)
}

func (b *Bank) Deposit(ctx context.Context, userID, accountID, amount int64) (domain.Transfer, error) {
	if amount <= 0 {
		return domain.Transfer{}, domain.ErrInvalidAmount
	}
	var tr domain.Transfer
	err := b.tx.WithinTx(ctx, func(ctx context.Context) error {
		acc, err := b.accounts.ByIDForUpdate(ctx, accountID)
		if err != nil {
			return err
		}
		if acc.UserID != userID {
			return domain.ErrNotYourAccount
		}
		if err := b.accounts.AddBalance(ctx, accountID, amount); err != nil {
			return err
		}
		tr, err = b.transfers.Create(ctx, nil, accountID, amount)
		return err
	})
	return tr, err
}

// Transfer moves money between two accounts atomically. The interesting part
// is deadlock avoidance: locks are always taken in ascending account-id
// order, so two opposite transfers (A→B and B→A) can never each hold the
// lock the other needs.
func (b *Bank) Transfer(ctx context.Context, userID, fromID, toID, amount int64) (domain.Transfer, error) {
	if amount <= 0 {
		return domain.Transfer{}, domain.ErrInvalidAmount
	}
	if fromID == toID {
		return domain.Transfer{}, domain.ErrSameAccount
	}

	var tr domain.Transfer
	err := b.tx.WithinTx(ctx, func(ctx context.Context) error {
		firstID, secondID := min(fromID, toID), max(fromID, toID)

		first, err := b.accounts.ByIDForUpdate(ctx, firstID)
		if err != nil {
			return err
		}
		second, err := b.accounts.ByIDForUpdate(ctx, secondID)
		if err != nil {
			return err
		}

		from, to := first, second
		if from.ID != fromID {
			from, to = second, first
		}

		switch {
		case from.UserID != userID:
			return domain.ErrNotYourAccount
		case from.Currency != to.Currency:
			return domain.ErrCurrencyMismatch
		case from.Balance < amount:
			return fmt.Errorf("balance %d, need %d: %w", from.Balance, amount, domain.ErrInsufficientFunds)
		}

		if err := b.accounts.AddBalance(ctx, from.ID, -amount); err != nil {
			return err
		}
		if err := b.accounts.AddBalance(ctx, to.ID, amount); err != nil {
			return err
		}
		tr, err = b.transfers.Create(ctx, &from.ID, to.ID, amount)
		return err
	})
	return tr, err
}

func (b *Bank) History(ctx context.Context, userID, accountID int64) ([]domain.Transfer, error) {
	acc, err := b.accounts.ByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if acc.UserID != userID {
		return nil, domain.ErrNotYourAccount
	}
	return b.transfers.History(ctx, accountID)
}
