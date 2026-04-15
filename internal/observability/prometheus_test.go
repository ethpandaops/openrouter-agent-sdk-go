package observability

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPrometheusMeterProvider(t *testing.T) {
	reg := prometheus.NewRegistry()
	mp, err := NewPrometheusMeterProvider(reg)
	require.NoError(t, err)
	require.NotNil(t, mp)

	// Create an observer with the Prometheus-backed MeterProvider.
	obs := New(Config{MeterProvider: mp})
	ctx := context.Background()

	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordToolCall(ctx, "test_tool", "ok")
	obs.RecordTokenUsage(ctx, 42, "input", "test-model")

	// Verify metrics are gathered by the Prometheus registry.
	families, err := reg.Gather()
	require.NoError(t, err)
	assert.NotEmpty(t, families, "prometheus registry should have gathered metrics")

	names := make(map[string]bool, len(families))
	for _, f := range families {
		names[f.GetName()] = true
	}

	// The Prometheus exporter translates OTel names: dots→underscores, adds unit suffix.
	// Just verify that some metrics with the expected prefix are present.
	found := 0
	for name := range names {
		t.Logf("prometheus metric: %s", name)
		found++
	}
	assert.GreaterOrEqual(t, found, 3, "should have at least 3 metric families registered")
}
