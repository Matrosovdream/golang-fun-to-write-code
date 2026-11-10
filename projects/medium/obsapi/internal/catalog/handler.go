package catalog

import (
	"encoding/json"
	"errors"
	"net/http"

	"obsapi/internal/obs"
)

// NewHandler decorates the repo here, in one place, so tests and main get
// identical instrumentation.
func NewHandler(o *obs.Obs, repo Repository) http.Handler {
	tracer := o.TP.Tracer("obsapi/catalog")
	svc := &Service{repo: tracedRepo{next: repo, tracer: tracer}, tracer: tracer, log: o.Log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /products/{id}", func(w http.ResponseWriter, r *http.Request) {
		p, err := svc.Get(r.Context(), r.PathValue("id"))
		respond(w, p, err)
	})
	mux.HandleFunc("GET /products", func(w http.ResponseWriter, r *http.Request) {
		ps, err := svc.List(r.Context())
		respond(w, ps, err)
	})
	return o.Handler(mux)
}

func respond(w http.ResponseWriter, v any, err error) {
	code := http.StatusOK
	switch {
	case err == nil:
	case errors.Is(err, ErrNotFound):
		code, v = http.StatusNotFound, map[string]string{"error": err.Error()}
	default: // the cause stays in the log line and the trace, not in the response
		code, v = http.StatusInternalServerError, map[string]string{"error": "internal error"}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) // a write error here means the client left
}
