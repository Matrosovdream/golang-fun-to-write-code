// Package obstest reads metric series back out of a registry, for assertions.
package obstest

import (
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

// Value sums the counter or gauge series in family name whose labels include
// want (for a histogram: the observation counts). A missing series reads as 0.
func Value(t testing.TB, g prometheus.Gatherer, name string, want prometheus.Labels) float64 {
	t.Helper()
	var sum float64
	for _, m := range metrics(t, g, name) {
		if !matches(m, want) {
			continue
		}
		switch {
		case m.Counter != nil:
			sum += m.Counter.GetValue()
		case m.Gauge != nil:
			sum += m.Gauge.GetValue()
		case m.Histogram != nil:
			sum += float64(m.Histogram.GetSampleCount())
		}
	}
	return sum
}

// LabelValues returns the distinct, sorted values label takes in family name.
func LabelValues(t testing.TB, g prometheus.Gatherer, name, label string) []string {
	t.Helper()
	var out []string
	for _, m := range metrics(t, g, name) {
		for _, lp := range m.Label {
			if lp.GetName() == label && !slices.Contains(out, lp.GetValue()) {
				out = append(out, lp.GetValue())
			}
		}
	}
	slices.Sort(out)
	return out
}

func metrics(t testing.TB, g prometheus.Gatherer, name string) []*dto.Metric {
	t.Helper()
	families, err := g.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() == name {
			return f.Metric
		}
	}
	return nil
}

func matches(m *dto.Metric, want prometheus.Labels) bool {
	for k, v := range want {
		if !slices.ContainsFunc(m.Label, func(lp *dto.LabelPair) bool { return lp.GetName() == k && lp.GetValue() == v }) {
			return false
		}
	}
	return true
}
