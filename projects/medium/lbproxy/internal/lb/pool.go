package lb

import (
	"fmt"
	"net"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Pool is one named group of backends behind one balancing strategy.
type Pool struct {
	Name     string
	Strategy string
	Backends []*Backend
	Balancer Balancer
	Key      func(*http.Request) string // only set for consistent hashing
	Retries  int                        // extra attempts for idempotent requests
}

// Pick chooses among the backends that are available and not in tried, and
// says how many there were, so the caller knows whether a retry is possible.
func (p *Pool) Pick(r *http.Request, tried []*Backend) (b *Backend, candidates int) {
	now := time.Now()
	cands := make([]*Backend, 0, len(p.Backends))
	for _, b := range p.Backends {
		if b.Available(now) && !slices.Contains(tried, b) {
			cands = append(cands, b)
		}
	}
	if len(cands) == 0 {
		return nil, 0
	}
	key := ""
	if p.Key != nil {
		key = p.Key(r)
	}
	return p.Balancer.Pick(key, cands), len(cands)
}

// Report records a request outcome and reports whether it ejected b.
func (p *Pool) Report(b *Backend, ok bool) (ejected bool) {
	if pen, isPenalizer := p.Balancer.(interface{ Penalize(*Backend) }); isPenalizer && !ok {
		pen.Penalize(b)
	}
	return b.Report(ok, time.Now())
}

// NewKeyFunc parses a hash_key spec: "ip" (the default), "path" or "header:<name>".
func NewKeyFunc(spec string) (func(*http.Request) string, error) {
	header, isHeader := strings.CutPrefix(spec, "header:")
	switch {
	case spec == "" || spec == "ip":
		return func(r *http.Request) string {
			host, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				return r.RemoteAddr
			}
			return host
		}, nil
	case spec == "path":
		return func(r *http.Request) string { return r.URL.Path }, nil
	case isHeader && header != "":
		return func(r *http.Request) string { return r.Header.Get(header) }, nil
	}
	return nil, fmt.Errorf("unknown hash_key %q (want ip, path or header:<name>)", spec)
}
