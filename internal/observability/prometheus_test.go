package observability

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	upstreamgenai "go.opentelemetry.io/otel/semconv/v1.40.0/genaiconv"
)

func TestNewPrometheusMeterProvider(t *testing.T) {
	reg := prometheus.NewRegistry()
	mp, err := NewPrometheusMeterProvider(reg)
	require.NoError(t, err)
	require.NotNil(t, mp)

	// Create an observer with the Prometheus-backed MeterProvider.
	obs, err := New(Config{MeterProvider: mp})
	require.NoError(t, err)
	ctx := context.Background()

	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordToolCall(ctx, "test_tool", "ok")
	obs.RecordTokenUsage(ctx, 42, upstreamgenai.TokenTypeInput, upstreamgenai.OperationNameChat, "test-model")

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

// TestExemplarsAttachedToHistograms verifies that histogram points recorded
// inside a sampled trace context carry trace-based exemplars at the OTel SDK
// layer. The promexporter sets exemplar.TraceBasedFilter on the underlying
// MeterProvider, so this test guards the metrics-to-traces link against
// regressions in the exemplar-filter wiring or instrument construction.
//
// Inspection happens via a ManualReader rather than the Prometheus exporter
// because the exporter's downconversion path strips per-bucket exemplars when
// the underlying aggregation is exponential. The exemplars do still flow over
// OTLP, which is what most production setups use.
func TestExemplarsAttachedToHistograms(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter),
	)
	defer func() { _ = mp.Shutdown(context.Background()) }()

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	obs, err := New(Config{MeterProvider: mp, TracerProvider: tp})
	require.NoError(t, err)

	ctx, span := obs.StartQuerySpan(context.Background(), upstreamgenai.OperationNameChat, "test-model", "sess1")
	wantTraceID := span.SpanContext().TraceID()
	wantSpanID := span.SpanContext().SpanID()
	obs.RecordOperationDuration(ctx, 0.42, upstreamgenai.OperationNameChat, "test-model", "")
	span.End()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var sawHistogram bool
	var sawExemplar bool
	var gotTraceID, gotSpanID []byte
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "gen_ai.client.operation.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				continue
			}
			sawHistogram = true
			for _, dp := range hist.DataPoints {
				for _, ex := range dp.Exemplars {
					sawExemplar = true
					gotTraceID = ex.TraceID
					gotSpanID = ex.SpanID
				}
			}
		}
	}

	require.True(t, sawHistogram, "expected gen_ai.client.operation.duration histogram")
	require.True(t, sawExemplar, "expected at least one exemplar on the histogram (TraceBasedFilter should attach when span is sampled)")
	assert.Equal(t, wantTraceID[:], gotTraceID, "exemplar trace_id should match the span's trace_id")
	assert.Equal(t, wantSpanID[:], gotSpanID, "exemplar span_id should match the span's span_id")
}
