package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jmoiron/sqlx"

	"gobank/internal/domain"
)

type UserRepo struct{ db *DB }

func NewUserRepo(db *DB) *UserRepo { return &UserRepo{db: db} }

func (r *UserRepo) Create(ctx context.Context, email, passwordHash string) (domain.User, error) {
	var u domain.User
	err := sqlx.GetContext(ctx, r.db.q(ctx), &u,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2)
		 RETURNING id, email, password_hash, created_at`,
		email, passwordHash)

	// 23505 = unique_violation: translate the driver error into a domain
	// error so upper layers never import pgconn.
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return domain.User{}, domain.ErrEmailTaken
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("insert user: %w", err)
	}
	return u, nil
}

func (r *UserRepo) ByEmail(ctx context.Context, email string) (domain.User, error) {
	var u domain.User
	err := sqlx.GetContext(ctx, r.db.q(ctx), &u,
		`SELECT id, email, password_hash, created_at FROM users WHERE email = $1`, email)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.User{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return domain.User{}, fmt.Errorf("select user: %w", err)
	}
	return u, nil
}
