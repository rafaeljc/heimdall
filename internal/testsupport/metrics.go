package testsupport

import (
	"sort"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GetMetricValue retrieves the current value of a Counter or Gauge from the DefaultGatherer.
func GetMetricValue(t *testing.T, metricName string, labelFilter map[string]string) float64 {
	t.Helper()

	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// mfs is guaranteed to be sorted by name.
	idx := sort.Search(len(mfs), func(i int) bool {
		return mfs[i].GetName() >= metricName
	})

	if idx < len(mfs) && mfs[idx].GetName() == metricName {
		mf := mfs[idx]
		for _, m := range mf.GetMetric() {
			if matchesLabels(m, labelFilter) {
				if m.GetCounter() != nil {
					return m.GetCounter().GetValue()
				}
				if m.GetGauge() != nil {
					return m.GetGauge().GetValue()
				}
				if m.GetHistogram() != nil {
					// For histograms, we verify the sample count (number of events)
					return float64(m.GetHistogram().GetSampleCount())
				}
			}
		}
	}
	return 0
}

func matchesLabels(m *io_prometheus_client.Metric, filter map[string]string) bool {
	if len(filter) == 0 {
		return true
	}
	metricLabels := make(map[string]string)
	for _, pair := range m.GetLabel() {
		metricLabels[pair.GetName()] = pair.GetValue()
	}

	for k, v := range filter {
		if val, ok := metricLabels[k]; !ok || val != v {
			return false
		}
	}
	return true
}

// AssertMetricDelta asserts that a metric increased by exactly 'expectedDelta' during the execution of 'fn'.
func AssertMetricDelta(t *testing.T, metricName string, labels map[string]string, expectedDelta float64, fn func()) {
	t.Helper()

	initial := GetMetricValue(t, metricName, labels)
	fn()
	final := GetMetricValue(t, metricName, labels)

	diff := final - initial
	assert.Equal(t, expectedDelta, diff, "metric %s%v delta mismatch", metricName, labels)
}

// AssertMetricDeltaAsync asserts that a metric eventually increases by 'expectedDelta'.
// Useful for async operations like Pub/Sub or Background Workers.
func AssertMetricDeltaAsync(t *testing.T, metricName string, labels map[string]string, expectedDelta float64, fn func()) {
	t.Helper()

	initial := GetMetricValue(t, metricName, labels)

	// Trigger the action
	fn()

	// Wait for the metric to match the expected value
	require.Eventually(t, func() bool {
		current := GetMetricValue(t, metricName, labels)
		return current == initial+expectedDelta
	}, 2*time.Second, 50*time.Millisecond, "metric %s%v failed to reach expected delta +%.0f", metricName, labels, expectedDelta)
}

// AssertHistogramRecorded asserts that a histogram has recorded at least one sample.
func AssertHistogramRecorded(t *testing.T, metricName string, labels map[string]string) {
	t.Helper()

	count := GetMetricValue(t, metricName, labels)
	assert.Greater(t, count, 0.0, "histogram %s%v should have recorded samples", metricName, labels)
}
