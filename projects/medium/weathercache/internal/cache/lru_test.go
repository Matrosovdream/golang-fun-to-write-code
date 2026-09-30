package cache_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"weathercache/internal/cache"
)

func TestEvictsLeastRecentlyUsed(t *testing.T) {
	l := cache.New[string, int](2, time.Hour)

	l.Set("a", 1)
	l.Set("b", 2)
	_, ok := l.Get("a") // touch "a" so "b" becomes the oldest
	require.True(t, ok)

	l.Set("c", 3) // over capacity → evicts "b"

	_, ok = l.Get("b")
	require.False(t, ok)
	v, ok := l.Get("a")
	require.True(t, ok)
	require.Equal(t, 1, v)
	require.Equal(t, 2, l.Len())
}

func TestTTLExpiry(t *testing.T) {
	// A fake clock instead of time.Sleep: tests stay instant and exact.
	now := time.Now()
	clock := func() time.Time { return now }
	l := cache.New(2, time.Minute, cache.WithClock[string, int](clock))

	l.Set("a", 1)
	_, ok := l.Get("a")
	require.True(t, ok)

	now = now.Add(61 * time.Second)
	_, ok = l.Get("a")
	require.False(t, ok, "entry should have expired")
	require.Equal(t, 0, l.Len(), "expired entry should be removed on access")
}

func TestSetRefreshesTTL(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	l := cache.New(2, time.Minute, cache.WithClock[string, int](clock))

	l.Set("a", 1)
	now = now.Add(45 * time.Second)
	l.Set("a", 2) // rewrite refreshes the deadline
	now = now.Add(45 * time.Second)

	v, ok := l.Get("a")
	require.True(t, ok)
	require.Equal(t, 2, v)
}
