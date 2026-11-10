package proxy

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"lbproxy/internal/config"
	"lbproxy/internal/health"
	"lbproxy/internal/lb"
)

// snapshot is one immutable generation of the config, built from scratch on
// every reload together with its own backends, balancer state and checkers.
type snapshot struct {
	routes []route
	pools  []*lb.Pool // sorted by name, for stable /lb/status output
	stop   context.CancelFunc
}

type route struct {
	host, prefix string
	pool         *lb.Pool
}

func (p *Proxy) build(cfg *config.Config) (*snapshot, error) {
	pools := make(map[string]*lb.Pool, len(cfg.Pools))
	s := &snapshot{}
	for _, name := range slices.Sorted(maps.Keys(cfg.Pools)) {
		pool, err := newPool(name, cfg.Pools[name])
		if err != nil {
			return nil, fmt.Errorf("pool %q: %w", name, err)
		}
		pools[name] = pool
		s.pools = append(s.pools, pool)
	}
	for _, rc := range cfg.Routes {
		s.routes = append(s.routes, route{strings.ToLower(rc.Host), rc.Prefix, pools[rc.Pool]})
	}
	// Host-specific routes beat wildcard hosts; then the longest prefix wins.
	specific := func(r route) int {
		if r.host != "" {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(s.routes, func(a, b route) int {
		return cmp.Or(cmp.Compare(specific(b), specific(a)), cmp.Compare(len(b.prefix), len(a.prefix)))
	})

	// Nothing below can fail, so a half-built snapshot never leaks goroutines.
	ctx, stop := context.WithCancel(p.ctx)
	s.stop = stop
	for _, pool := range s.pools {
		if h := cfg.Pools[pool.Name].Health; h != nil {
			c := &health.Checker{Backends: pool.Backends, Path: h.Path, Timeout: h.Timeout,
				Client: &http.Client{Transport: p.transport}, Log: p.log.With("pool", pool.Name)}
			go func() {
				t := time.NewTicker(h.Interval)
				defer t.Stop()
				c.Run(ctx, t.C)
			}()
		}
	}
	return s, nil
}

func newPool(name string, pc config.Pool) (*lb.Pool, error) {
	pol := lb.Policy{MaxFails: pc.Passive.MaxFails, Cooldown: pc.Passive.Cooldown}
	if pc.Health != nil {
		pol.Rise, pol.Fall = pc.Health.Rise, pc.Health.Fall
	}
	backends := make([]*lb.Backend, len(pc.Backends))
	for i, bc := range pc.Backends {
		u, err := url.Parse(bc.URL)
		if err != nil {
			return nil, err
		}
		backends[i] = lb.NewBackend(u, bc.Weight, pol)
	}
	bal, err := lb.NewBalancer(pc.Strategy, backends)
	if err != nil {
		return nil, err
	}
	pool := &lb.Pool{Name: name, Strategy: pc.Strategy, Backends: backends, Balancer: bal, Retries: pc.Retries}
	if pc.Strategy == lb.ConsistentHash {
		if pool.Key, err = lb.NewKeyFunc(pc.HashKey); err != nil {
			return nil, err
		}
	}
	return pool, nil
}

func (s *snapshot) match(r *http.Request) *route {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	for i := range s.routes {
		if rt := &s.routes[i]; (rt.host == "" || rt.host == host) && strings.HasPrefix(r.URL.Path, rt.prefix) {
			return rt
		}
	}
	return nil
}
