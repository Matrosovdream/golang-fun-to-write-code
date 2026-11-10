package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"obsapi/internal/catalog"
	"obsapi/internal/obs"
)

func NewHandler(o *obs.Obs, catalogURL string) http.Handler {
	// o.Transport is where tracing enters: each call below becomes a client
	// span plus a traceparent header, and this file never mentions OpenTelemetry.
	c := client{base: catalogURL, http: &http.Client{Transport: o.Transport(http.DefaultTransport), Timeout: 2 * time.Second}}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		var p catalog.Product
		err := c.get(r.Context(), "/products/"+url.PathEscape(r.PathValue("id")), &p)
		respond(r.Context(), o.Log, w, p, err)
	})
	mux.HandleFunc("GET /products", func(w http.ResponseWriter, r *http.Request) {
		var ps []catalog.Product
		err := c.get(r.Context(), "/products", &ps)
		respond(r.Context(), o.Log, w, ps, err)
	})
	return o.Handler(mux)
}

func respond(ctx context.Context, log *slog.Logger, w http.ResponseWriter, v any, err error) {
	code := http.StatusOK
	switch {
	case err == nil:
	case errors.Is(err, catalog.ErrNotFound):
		code, v = http.StatusNotFound, map[string]string{"error": "product not found"}
	default:
		log.ErrorContext(ctx, "catalog call failed", "err", err)
		code, v = http.StatusBadGateway, map[string]string{"error": "catalog unavailable"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) // a write error here means the client left
}

type client struct {
	base string
	http *http.Client
}

func (c client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("call catalog: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("decode catalog response: %w", err)
		}
		return nil
	case http.StatusNotFound:
		return catalog.ErrNotFound
	default:
		return fmt.Errorf("catalog returned %s", resp.Status)
	}
}
