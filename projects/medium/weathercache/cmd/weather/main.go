package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"weathercache/internal/cache"
	"weathercache/internal/httpx"
	"weathercache/internal/weather"
)

func main() {
	ttl := flag.Duration("ttl", 5*time.Minute, "cache TTL")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: weather [flags] <city> [city...]")
		os.Exit(2)
	}

	// The onion, assembled in one place:
	// Cached → OpenMeteo → http.Client → RetryTransport → network.
	client := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &httpx.RetryTransport{MaxAttempts: 3, BaseDelay: 300 * time.Millisecond},
	}
	var provider weather.Provider = weather.NewOpenMeteo(weather.WithHTTPClient(client))
	provider = weather.NewCached(provider, cache.New[string, weather.Report](64, *ttl))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, city := range flag.Args() {
		// Each city is looked up twice to make the cache visible: the second
		// call should report microseconds instead of hundreds of ms.
		for i, label := range []string{"api  ", "cache"} {
			start := time.Now()
			r, err := provider.Current(ctx, city)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", city, err)
				break
			}
			if i == 0 {
				fmt.Printf("%s, %s: %.1f°C, wind %.0f km/h, %s\n",
					r.City, r.Country, r.TempC, r.WindKmh, r.Description)
			}
			fmt.Printf("   %s lookup took %10s\n", label, time.Since(start).Round(time.Microsecond))
		}
	}
}
