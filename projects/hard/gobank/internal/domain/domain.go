// Package domain holds the core types and business errors. It imports
// nothing from the other layers — dependencies only point inward.
package domain

import (
	"errors"
	"time"
)

var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrAccountNotFound    = errors.New("account not found")
	ErrNotYourAccount     = errors.New("account belongs to another user")
	ErrInsufficientFunds  = errors.New("insufficient funds")
	ErrCurrencyMismatch   = errors.New("accounts have different currencies")
	ErrSameAccount        = errors.New("cannot transfer to the same account")
	ErrInvalidAmount      = errors.New("amount must be positive")
)

type User struct {
	ID           int64     `db:"id"`
	Email        string    `db:"email"`
	PasswordHash string    `db:"password_hash"`
	CreatedAt    time.Time `db:"created_at"`
}

// Money is stored as int64 minor units (cents). Never float64: 0.1+0.2
// arithmetic has no place in a ledger.
type Account struct {
	ID        int64     `db:"id" json:"id"`
	UserID    int64     `db:"user_id" json:"user_id"`
	Currency  string    `db:"currency" json:"currency"`
	Balance   int64     `db:"balance" json:"balance"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// FromAccountID is nil for deposits — money entering the system.
type Transfer struct {
	ID            int64     `db:"id" json:"id"`
	FromAccountID *int64    `db:"from_account_id" json:"from_account_id"`
	ToAccountID   int64     `db:"to_account_id" json:"to_account_id"`
	Amount        int64     `db:"amount" json:"amount"`
	CreatedAt     time.Time `db:"created_at" json:"created_at"`
}
