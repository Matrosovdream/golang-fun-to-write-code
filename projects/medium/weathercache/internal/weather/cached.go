package weather

import (
	"context"
	"strings"

	"weathercache/internal/cache"
)

// Cached is a decorator: it satisfies Provider and wraps another Provider,
// adding caching without the wrapped code or callers changing at all.
type Cached struct {
	next  Provider
	cache *cache.LRU[string, Report]
}

func NewCached(next Provider, c *cache.LRU[string, Report]) *Cached {
	return &Cached{next: next, cache: c}
}

func (c *Cached) Current(ctx context.Context, city string) (Report, error) {
	key := strings.ToLower(strings.TrimSpace(city))
	if r, ok := c.cache.Get(key); ok {
		return r, nil
	}

	r, err := c.next.Current(ctx, city)
	if err != nil {
		return Report{}, err // errors are not cached — the next call retries
	}
	c.cache.Set(key, r)
	return r, nil
}
