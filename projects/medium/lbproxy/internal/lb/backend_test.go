package lb

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newBackends(n int, pol Policy, weights ...int) []*Backend {
	bs := make([]*Backend, n)
	for i := range bs {
		w := 1
		if i < len(weights) {
			w = weights[i]
		}
		bs[i] = NewBackend(&url.URL{Scheme: "http", Host: "10.0.0." + strconv.Itoa(i+1) + ":80"}, w, pol)
	}
	return bs
}

func TestObserveHysteresis(t *testing.T) {
	// P = good probe, F = failed probe. Fall=3, Rise=2.
	tests := []struct {
		name   string
		probes string
		want   bool // healthy at the end
	}{
		{"starts healthy", "", true},
		{"two failures are not enough", "FF", true},
		{"three in a row go down", "FFF", false},
		{"a good probe resets the failure streak", "FFPFF", true},
		{"one good probe does not bring it back", "FFFP", false},
		{"two in a row do", "FFFPP", true},
		{"a failure resets the recovery streak", "FFFPFP", false},
		{"flapping never changes state", "FPFPFPFPFP", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := newBackends(1, Policy{Rise: 2, Fall: 3})[0]
			for _, p := range tt.probes {
				b.Observe(p == 'P')
			}
			require.Equal(t, tt.want, b.Healthy())
		})
	}
}

func TestObserveReportsChangeOnce(t *testing.T) {
	b := newBackends(1, Policy{Rise: 1, Fall: 2})[0]
	require.False(t, b.Observe(false))
	require.True(t, b.Observe(false), "second failure flips it")
	require.False(t, b.Observe(false), "already down")
	require.True(t, b.Observe(true))
}

func TestPassiveEjectionAndHalfOpen(t *testing.T) {
	b := newBackends(1, Policy{MaxFails: 3, Cooldown: time.Minute})[0]
	now := time.Unix(1000, 0)

	require.False(t, b.Report(false, now))
	require.False(t, b.Report(false, now))
	require.True(t, b.Available(now), "two failures: below the threshold")
	b.Report(true, now) // a success resets the consecutive count
	require.False(t, b.Report(false, now))
	require.False(t, b.Report(false, now))
	require.True(t, b.Report(false, now), "third consecutive failure ejects")
	require.False(t, b.Available(now))
	require.False(t, b.Available(now.Add(59*time.Second)))

	after := now.Add(time.Minute)
	require.True(t, b.Available(after), "cooldown over: half-open")
	require.True(t, b.Report(false, after), "one more failure ejects again at once")
	require.False(t, b.Available(after))

	later := after.Add(time.Minute)
	b.Report(true, later) // a success while half-open closes it
	require.False(t, b.Report(false, later), "failure count started over")
	require.True(t, b.Available(later))
}

func TestEjectionDoesNotTouchHealthState(t *testing.T) {
	b := newBackends(1, Policy{MaxFails: 1, Cooldown: time.Minute})[0]
	now := time.Now()
	b.Report(false, now)
	require.True(t, b.Healthy(), "ejection is passive; the active verdict is independent")
	require.False(t, b.Available(now))
	require.True(t, b.Stats(now).Ejected)
}

func TestPoolPickSkipsUnavailableAndTried(t *testing.T) {
	bs := newBackends(3, Policy{MaxFails: 1, Cooldown: time.Hour, Fall: 1, Rise: 1})
	bal, err := NewBalancer(RoundRobin, bs)
	require.NoError(t, err)
	p := &Pool{Backends: bs, Balancer: bal}
	r := httptest.NewRequest("GET", "/", nil)

	bs[0].Observe(false) // down by active check
	bs[1].Report(false, time.Now())
	b, n := p.Pick(r, nil)
	require.Same(t, bs[2], b)
	require.Equal(t, 1, n)

	b, n = p.Pick(r, []*Backend{bs[2]})
	require.Nil(t, b)
	require.Zero(t, n)
}

func TestNewBalancerRejectsUnknown(t *testing.T) {
	_, err := NewBalancer("random", nil)
	require.ErrorContains(t, err, `unknown strategy "random"`)
}

func TestKeyFunc(t *testing.T) {
	r := httptest.NewRequest("GET", "/some/path", nil)
	r.RemoteAddr = "192.0.2.7:5555"
	r.Header.Set("X-User", "alice")
	tests := map[string]string{"": "192.0.2.7", "ip": "192.0.2.7", "path": "/some/path", "header:X-User": "alice"}
	for spec, want := range tests {
		f, err := NewKeyFunc(spec)
		require.NoError(t, err, spec)
		require.Equal(t, want, f(r), spec)
	}
	for _, spec := range []string{"cookie", "header:", "IP"} {
		_, err := NewKeyFunc(spec)
		require.Error(t, err, spec)
	}
}
