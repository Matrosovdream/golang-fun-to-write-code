package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const minimal = `
listen: 127.0.0.1:0
routes:
  - pool: web
pools:
  web:
    backends:
      - url: http://127.0.0.1:9101
`

func TestDefaults(t *testing.T) {
	c, err := Parse([]byte(minimal))
	require.NoError(t, err)
	require.Equal(t, "/", c.Routes[0].Prefix)
	p := c.Pools["web"]
	require.Equal(t, "round_robin", p.Strategy)
	require.Equal(t, 1, p.Backends[0].Weight)
	require.Equal(t, 3, p.Passive.MaxFails)
	require.Equal(t, 10*time.Second, p.Passive.Cooldown)
	require.Nil(t, p.Health, "no health block, no active checks")
}

func TestHealthDefaults(t *testing.T) {
	c, err := Parse([]byte(minimal + "    health: {interval: 1s}\n"))
	require.NoError(t, err)
	require.Equal(t, &Health{Path: "/healthz", Interval: time.Second, Timeout: time.Second, Rise: 2, Fall: 3}, c.Pools["web"].Health)

	c, err = Parse([]byte(minimal + "    health: {}\n"))
	require.NoError(t, err)
	require.Equal(t, &Health{Path: "/healthz", Interval: 5 * time.Second, Timeout: 2 * time.Second, Rise: 2, Fall: 3}, c.Pools["web"].Health)
}

func TestShippedExampleIsValid(t *testing.T) {
	c, err := Load("../../lb.example.yaml")
	require.NoError(t, err)
	require.Len(t, c.Pools, 4)
	require.Equal(t, "header:X-User", c.Pools["sticky"].HashKey)
	require.Equal(t, 5, c.Pools["heavy"].Backends[0].Weight)
}

func TestInvalid(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"not yaml", "listen: [", "parse config"},
		{"empty", "", "parse config"},
		{"typo in a key", minimal + "    retires: 2\n", "field retires not found"},
		{"no listen", "routes: [{pool: web}]\npools: {web: {backends: [{url: 'http://a:1'}]}}", "listen is required"},
		{"no routes", "listen: :1\npools: {web: {backends: [{url: 'http://a:1'}]}}", "at least one route"},
		{"unknown pool", "listen: :1\nroutes: [{pool: nope}]\npools: {web: {backends: [{url: 'http://a:1'}]}}", `unknown pool "nope"`},
		{"prefix without slash", "listen: :1\nroutes: [{pool: web, path_prefix: api}]\npools: {web: {backends: [{url: 'http://a:1'}]}}", "must start with /"},
		{"unknown strategy", minimal + "    strategy: random\n", `unknown strategy "random"`},
		{"bad hash key", minimal + "    hash_key: cookie\n", `unknown hash_key "cookie"`},
		{"negative retries", minimal + "    retries: -1\n", "retries must not be negative"},
		{"no backends", "listen: :1\nroutes: [{pool: web}]\npools: {web: {strategy: round_robin}}", "at least one backend"},
		{"not a url", "listen: :1\nroutes: [{pool: web}]\npools: {web: {backends: [{url: '::'}]}}", "is not an http(s) URL"},
		{"wrong scheme", "listen: :1\nroutes: [{pool: web}]\npools: {web: {backends: [{url: 'ftp://a:1'}]}}", "is not an http(s) URL"},
		{"no host", "listen: :1\nroutes: [{pool: web}]\npools: {web: {backends: [{url: 'http://'}]}}", "is not an http(s) URL"},
		{"negative weight", "listen: :1\nroutes: [{pool: web}]\npools: {web: {backends: [{url: 'http://a:1', weight: -2}]}}", "weight must not be negative"},
		{"timeout above interval", minimal + "    health: {interval: 1s, timeout: 2s}\n", "timeout <= interval"},
		{"bad duration", minimal + "    passive: {cooldown: soon}\n", "parse config"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, err := Parse([]byte(tt.yaml))
			require.Nil(t, c)
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestAllProblemsAreReported(t *testing.T) {
	_, err := Parse([]byte(`
routes: [{pool: nope}]
pools:
  a: {strategy: random, backends: []}
`))
	require.Error(t, err)
	for _, want := range []string{"listen is required", `unknown pool "nope"`, `unknown strategy "random"`, "at least one backend"} {
		require.ErrorContains(t, err, want)
	}
}
