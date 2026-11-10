// Package config loads and fully validates the YAML configuration. Parse
// either returns a config that is safe to build a proxy from or an error that
// lists every problem found, which is what makes hot reload safe: a bad file
// never gets as far as touching the running proxy.
package config

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"lbproxy/internal/lb"
)

type Config struct {
	Listen string          `yaml:"listen"`
	Routes []Route         `yaml:"routes"`
	Pools  map[string]Pool `yaml:"pools"`
}

// Route matches on Host (any when empty) and path prefix (default "/").
type Route struct {
	Host   string `yaml:"host"`
	Prefix string `yaml:"path_prefix"`
	Pool   string `yaml:"pool"`
}

type Pool struct {
	Strategy string    `yaml:"strategy"`
	HashKey  string    `yaml:"hash_key"`
	Retries  int       `yaml:"retries"` // extra attempts for GET/HEAD/OPTIONS; 0 disables
	Backends []Backend `yaml:"backends"`
	Health   *Health   `yaml:"health"` // active checks run only when this block exists
	Passive  Passive   `yaml:"passive"`
}

type Backend struct {
	URL    string `yaml:"url"`
	Weight int    `yaml:"weight"`
}

type Health struct {
	Path     string        `yaml:"path"`
	Interval time.Duration `yaml:"interval"`
	Timeout  time.Duration `yaml:"timeout"`
	Rise     int           `yaml:"rise"`
	Fall     int           `yaml:"fall"`
}

type Passive struct {
	MaxFails int           `yaml:"max_fails"`
	Cooldown time.Duration `yaml:"cooldown"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(data)
}

func Parse(data []byte) (*Config, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // a typo'd key is an error, not a silently ignored setting
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("invalid config:\n%w", err)
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	for i := range c.Routes {
		c.Routes[i].Prefix = cmp.Or(c.Routes[i].Prefix, "/")
	}
	for name, p := range c.Pools {
		p.Strategy = cmp.Or(p.Strategy, lb.RoundRobin)
		for i := range p.Backends {
			p.Backends[i].Weight = cmp.Or(p.Backends[i].Weight, 1)
		}
		p.Passive.MaxFails = cmp.Or(p.Passive.MaxFails, 3)
		p.Passive.Cooldown = cmp.Or(p.Passive.Cooldown, 10*time.Second)
		if h := p.Health; h != nil {
			h.Path = cmp.Or(h.Path, "/healthz")
			h.Interval = cmp.Or(h.Interval, 5*time.Second)
			h.Timeout = cmp.Or(h.Timeout, min(2*time.Second, h.Interval))
			h.Rise = cmp.Or(h.Rise, 2)
			h.Fall = cmp.Or(h.Fall, 3)
		}
		c.Pools[name] = p
	}
}

func (c *Config) validate() error {
	var errs []error
	bad := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }

	if c.Listen == "" {
		bad("listen is required")
	}
	if len(c.Routes) == 0 {
		bad("at least one route is required")
	}
	for i, r := range c.Routes {
		if _, ok := c.Pools[r.Pool]; !ok {
			bad("routes[%d]: unknown pool %q", i, r.Pool)
		}
		if !strings.HasPrefix(r.Prefix, "/") {
			bad("routes[%d]: path_prefix %q must start with /", i, r.Prefix)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(c.Pools)) {
		p := c.Pools[name]
		if !slices.Contains(lb.Strategies, p.Strategy) {
			bad("pool %q: unknown strategy %q (want one of %v)", name, p.Strategy, lb.Strategies)
		}
		if _, err := lb.NewKeyFunc(p.HashKey); err != nil {
			bad("pool %q: %v", name, err)
		}
		if p.Retries < 0 {
			bad("pool %q: retries must not be negative", name)
		}
		if len(p.Backends) == 0 {
			bad("pool %q: at least one backend is required", name)
		}
		for i, b := range p.Backends {
			if u, err := url.Parse(b.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				bad("pool %q: backends[%d]: %q is not an http(s) URL", name, i, b.URL)
			}
			if b.Weight < 0 {
				bad("pool %q: backends[%d]: weight must not be negative", name, i)
			}
		}
		if p.Passive.MaxFails < 0 || p.Passive.Cooldown < 0 {
			bad("pool %q: passive values must not be negative", name)
		}
		if h := p.Health; h != nil && (h.Interval < 0 || h.Timeout < 0 || h.Timeout > h.Interval || h.Rise < 0 || h.Fall < 0) {
			bad("pool %q: health needs non-negative values and timeout <= interval", name)
		}
	}
	return errors.Join(errs...)
}
