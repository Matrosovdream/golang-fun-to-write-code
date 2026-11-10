package main

import (
	"flag"
	"log"
	"net/http"

	"obsapi/internal/catalog"
	"obsapi/internal/obs"
)

func main() {
	var cfg obs.Config
	cfg.Bind(":8105")
	failRate := flag.Float64("fail-rate", 0, "simulated repository failure probability, 0..1")
	flag.Parse()

	if err := obs.Run("catalog", cfg, func(o *obs.Obs) http.Handler {
		repo := catalog.NewMemRepo(catalog.Seed()...)
		repo.FailRate = *failRate
		o.Metrics.Registry.MustRegister(catalog.NewCollector(repo))
		return catalog.NewHandler(o, repo)
	}); err != nil {
		log.Fatal(err)
	}
}
