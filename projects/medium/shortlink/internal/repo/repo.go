package repo

import (
	"context"
	"errors"

	"shortlink/internal/link"
)

var (
	ErrNotFound  = errors.New("link not found")
	ErrCodeTaken = errors.New("code already taken")
)

type Repo interface {
	Save(ctx context.Context, l link.Link) error
	Get(ctx context.Context, code string) (link.Link, error)
	// Touch increments the hit counter atomically in the backend, instead of
	// read-modify-write in the service — that would race under concurrency.
	Touch(ctx context.Context, code string) error
}
