package proxy

import (
	"encoding/json"
	"expvar"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"lbproxy/internal/lb"
)

// Handler is the full server: the admin endpoints, and everything else
// through the proxy wrapped in the instrument decorator. Both admin paths are
// reserved on the proxy's own port, so they are visible to proxied clients;
// a production setup would bind them to a separate listener.
func (p *Proxy) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /lb/status", p.serveStatus)
	mux.Handle("GET /debug/vars", expvar.Handler())
	mux.Handle("/", instrument(p.log, p))
	return mux
}

type poolStatus struct {
	Name     string     `json:"name"`
	Strategy string     `json:"strategy"`
	Backends []lb.Stats `json:"backends"`
}

type status struct {
	Pools    []poolStatus     `json:"pools"`
	Counters map[string]int64 `json:"counters"`
}

func (p *Proxy) serveStatus(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	st := status{Counters: map[string]int64{}}
	for _, pool := range p.snap.Load().pools {
		ps := poolStatus{Name: pool.Name, Strategy: pool.Strategy}
		for _, b := range pool.Backends {
			ps.Backends = append(ps.Backends, b.Stats(now))
		}
		st.Pools = append(st.Pools, ps)
	}
	counters.Do(func(kv expvar.KeyValue) { st.Counters[kv.Key] = kv.Value.(*expvar.Int).Value() })

	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.Encode(st)
}

// instrument is a decorator: it wraps a handler to count and log requests
// without the proxy knowing.
func instrument(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		counters.Add("requests", 1)
		counters.Add("responses_"+strconv.Itoa(rec.status/100)+"xx", 1)
		log.Info("request", "method", r.Method, "host", r.Host, "path", r.URL.Path,
			"status", rec.status, "dur", time.Since(start).Round(time.Microsecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the real writer, which is how
// ReverseProxy flushes streaming responses and hijacks upgrades through us.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }
