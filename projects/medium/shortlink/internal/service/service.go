package service

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/url"
	"time"

	"shortlink/internal/link"
	"shortlink/internal/repo"
)

var ErrInvalidURL = errors.New("invalid url: must be absolute http(s)")

const (
	alphabet   = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLen    = 7
	maxRetries = 5
)

type Service struct {
	repo repo.Repo
}

func New(r repo.Repo) *Service {
	return &Service{repo: r}
}

func (s *Service) Shorten(ctx context.Context, rawURL string) (link.Link, error) {
	if !validURL(rawURL) {
		return link.Link{}, fmt.Errorf("%q: %w", rawURL, ErrInvalidURL)
	}

	l := link.Link{URL: rawURL, CreatedAt: time.Now()}
	// 62^7 ≈ 3.5 trillion codes, so a collision is rare — but it must still
	// be handled, and retrying on ErrCodeTaken is simpler than coordinating.
	for range maxRetries {
		l.Code = randomCode()
		err := s.repo.Save(ctx, l)
		if err == nil {
			return l, nil
		}
		if !errors.Is(err, repo.ErrCodeTaken) {
			return link.Link{}, err
		}
	}
	return link.Link{}, fmt.Errorf("could not find a free code in %d tries", maxRetries)
}

// Resolve returns the target URL and counts the hit.
func (s *Service) Resolve(ctx context.Context, code string) (string, error) {
	l, err := s.repo.Get(ctx, code)
	if err != nil {
		return "", err
	}
	if err := s.repo.Touch(ctx, code); err != nil {
		return "", err
	}
	return l.URL, nil
}

func (s *Service) Stats(ctx context.Context, code string) (link.Link, error) {
	return s.repo.Get(ctx, code)
}

func validURL(raw string) bool {
	u, err := url.ParseRequestURI(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func randomCode() string {
	b := make([]byte, codeLen)
	for i := range b {
		b[i] = alphabet[rand.IntN(len(alphabet))]
	}
	return string(b)
}
