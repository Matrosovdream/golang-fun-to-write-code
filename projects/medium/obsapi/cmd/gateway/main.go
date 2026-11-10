package main

import (
	"flag"
	"log"
	"net/http"

	"obsapi/internal/gateway"
	"obsapi/internal/obs"
)

func main() {
	var cfg obs.Config
	cfg.Bind(":8104")
	catalogURL := flag.String("catalog", "http://localhost:8105", "catalog base URL")
	flag.Parse()

	if err := obs.Run("gateway", cfg, func(o *obs.Obs) http.Handler {
		return gateway.NewHandler(o, *catalogURL)
	}); err != nil {
		log.Fatal(err)
	}
}
