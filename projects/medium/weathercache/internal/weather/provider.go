package weather

import (
	"context"
	"errors"
	"time"
)

var ErrCityNotFound = errors.New("city not found")

type Report struct {
	City        string
	Country     string
	Lat, Lon    float64
	TempC       float64
	WindKmh     float64
	Description string
	FetchedAt   time.Time
}

// Provider is the seam of the whole project: the real API client, the
// caching decorator, and the test mock all satisfy it.
//
//go:generate go run github.com/vektra/mockery/v2@v2.53.3 --name Provider --dir . --output ./mocks --outpkg mocks
type Provider interface {
	Current(ctx context.Context, city string) (Report, error)
}
