package service

import (
	"context"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"gobank/internal/domain"
	"gobank/internal/repo"
)

type Auth struct {
	users  repo.Users
	tokens *TokenManager
}

func NewAuth(users repo.Users, tokens *TokenManager) *Auth {
	return &Auth{users: users, tokens: tokens}
}

func (a *Auth) Register(ctx context.Context, email, password string) (string, error) {
	// bcrypt embeds the salt in the hash; DefaultCost is deliberately slow —
	// that slowness is the security property.
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	u, err := a.users.Create(ctx, email, string(hash))
	if err != nil {
		return "", err
	}
	return a.tokens.Issue(u.ID)
}

func (a *Auth) Login(ctx context.Context, email, password string) (string, error) {
	u, err := a.users.ByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidCredentials) {
			// Same work and same answer whether the email exists or the
			// password is wrong — no user enumeration.
			_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
			return "", domain.ErrInvalidCredentials
		}
		return "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", domain.ErrInvalidCredentials
	}
	return a.tokens.Issue(u.ID)
}

var dummyHash = []byte("$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy")
