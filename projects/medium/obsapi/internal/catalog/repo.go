package catalog

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/prometheus/client_golang/prometheus"
)

type Product struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	PriceCents int    `json:"price_cents"`
}

var (
	ErrNotFound    = errors.New("product not found")
	ErrUnavailable = errors.New("repository unavailable")
)

type Repository interface {
	Get(ctx context.Context, id string) (Product, error)
	List(ctx context.Context) ([]Product, error)
}

// MemRepo stands in for a database; FailRate makes it flaky to produce error spans and 5xx series.
type MemRepo struct {
	FailRate float64 // probability that a call fails with ErrUnavailable

	products map[string]Product
	reads    atomic.Int64
}

func NewMemRepo(products ...Product) *MemRepo {
	r := &MemRepo{products: make(map[string]Product, len(products))}
	for _, p := range products {
		r.products[p.ID] = p
	}
	return r
}

func Seed() []Product {
	return []Product{
		{"p-1", "Espresso mug", 1250},
		{"p-2", "Gopher plush", 2400},
		{"p-3", "Mechanical keyboard", 11900},
		{"p-4", "Desk mat", 3100},
	}
}

func (r *MemRepo) Get(_ context.Context, id string) (Product, error) {
	if err := r.read(); err != nil {
		return Product{}, err
	}
	p, ok := r.products[id]
	if !ok {
		return Product{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return p, nil
}

func (r *MemRepo) List(context.Context) ([]Product, error) {
	if err := r.read(); err != nil {
		return nil, err
	}
	out := make([]Product, 0, len(r.products))
	for _, p := range r.products {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b Product) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}

func (r *MemRepo) read() error {
	r.reads.Add(1)
	if rand.Float64() < r.FailRate {
		return ErrUnavailable
	}
	return nil
}

// Collector reports the repo's own numbers at scrape time. A custom Collector
// fits when the number already lives elsewhere (an atomic in MemRepo): expose it, don't mirror it.
type Collector struct {
	repo     *MemRepo
	products *prometheus.Desc
	reads    *prometheus.Desc
}

func NewCollector(repo *MemRepo) *Collector {
	return &Collector{
		repo:     repo,
		products: prometheus.NewDesc("catalog_products", "Products currently in the catalog.", nil, nil),
		reads:    prometheus.NewDesc("catalog_repo_reads_total", "Repository read calls, failed ones included.", nil, nil),
	}
}

func (c *Collector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.products
	ch <- c.reads
}

func (c *Collector) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(c.products, prometheus.GaugeValue, float64(len(c.repo.products)))
	ch <- prometheus.MustNewConstMetric(c.reads, prometheus.CounterValue, float64(c.repo.reads.Load()))
}
