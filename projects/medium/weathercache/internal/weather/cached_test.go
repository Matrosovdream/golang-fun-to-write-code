package weather_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"weathercache/internal/cache"
	"weathercache/internal/weather"
	"weathercache/internal/weather/mocks"
)

func TestCachedServesSecondCallFromCache(t *testing.T) {
	next := mocks.NewProvider(t)
	next.On("Current", mock.Anything, "Berlin").
		Return(weather.Report{City: "Berlin", TempC: 21}, nil).
		Once() // the test fails if the API is hit twice

	c := weather.NewCached(next, cache.New[string, weather.Report](10, time.Minute))

	for range 2 {
		r, err := c.Current(context.Background(), "Berlin")
		require.NoError(t, err)
		require.Equal(t, "Berlin", r.City)
	}
	// "berlin " normalizes to the same key:
	_, err := c.Current(context.Background(), "berlin ")
	require.NoError(t, err)
}

func TestCachedExpiryHitsUpstreamAgain(t *testing.T) {
	next := mocks.NewProvider(t)
	next.On("Current", mock.Anything, "Oslo").
		Return(weather.Report{City: "Oslo"}, nil).
		Twice()

	now := time.Now()
	lru := cache.New(10, time.Minute,
		cache.WithClock[string, weather.Report](func() time.Time { return now }))
	c := weather.NewCached(next, lru)

	_, err := c.Current(context.Background(), "Oslo")
	require.NoError(t, err)

	now = now.Add(2 * time.Minute) // TTL passes → cache miss → upstream again
	_, err = c.Current(context.Background(), "Oslo")
	require.NoError(t, err)
}

func TestCachedDoesNotCacheErrors(t *testing.T) {
	boom := errors.New("api down")
	next := mocks.NewProvider(t)
	next.On("Current", mock.Anything, "Nowhere").
		Return(weather.Report{}, boom).
		Twice()

	c := weather.NewCached(next, cache.New[string, weather.Report](10, time.Minute))

	for range 2 {
		_, err := c.Current(context.Background(), "Nowhere")
		require.ErrorIs(t, err, boom)
	}
}
