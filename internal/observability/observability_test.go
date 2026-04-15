package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestNoopObserverDoesNotPanic(t *testing.T) {
	obs := Noop()
	ctx := context.Background()

	// None of these should panic.
	obs.RecordOperationDuration(ctx, 1.5, "test-model", "sess1", "")
	obs.RecordTokenUsage(ctx, 100, "input", "test-model")
	obs.RecordTokenUsage(ctx, 0, "output", "test-model") // zero — should noop
	obs.RecordTTFT(ctx, 0.5, "test-model")
	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordRateLimitEvent(ctx)
	obs.RecordToolCall(ctx, "tool1", "ok")
	obs.RecordToolCallDuration(ctx, 0.1, "tool1")
	obs.RecordCheckpointOp(ctx, "create", "ok")
	obs.RecordHookDuration(ctx, 0.01, "pre_tool_use")

	// Span creation should return valid (noop) spans.
	_, span := obs.StartQuerySpan(ctx, "model", "sess")
	span.End()
	_, span = obs.StartHTTPSpan(ctx, "POST", "/chat/completions")
	span.End()
	_, span = obs.StartToolSpan(ctx, "tool1")
	span.End()
	_, span = obs.StartHookSpan(ctx, "pre_tool_use")
	span.End()
}

func TestNewWithNilProvidersIsNoop(t *testing.T) {
	obs := New(Config{})
	assert.NotNil(t, obs)

	ctx := context.Background()
	obs.RecordHTTPRequest(ctx, "2xx", false)
}

func TestStatusClass(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{201, "2xx"},
		{299, "2xx"},
		{301, "3xx"},
		{400, "4xx"},
		{404, "4xx"},
		{429, "4xx"},
		{500, "5xx"},
		{503, "5xx"},
		{100, "other"},
		{0, "other"},
	}
	for _, tt := range tests {
		got := StatusClass(tt.code)
		assert.Equal(t, tt.want, got, "StatusClass(%d)", tt.code)
	}
}

func TestMetricsRecordedWithRealProvider(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	obs := New(Config{MeterProvider: mp})
	ctx := context.Background()

	// Record various metrics.
	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordHTTPRequest(ctx, "4xx", true)
	obs.RecordRateLimitEvent(ctx)
	obs.RecordToolCall(ctx, "my_tool", "ok")
	obs.RecordToolCall(ctx, "my_tool", "error")
	obs.RecordToolCallDuration(ctx, 0.5, "my_tool")
	obs.RecordCheckpointOp(ctx, "create", "ok")
	obs.RecordCheckpointOp(ctx, "restore", "no_checkpoint")
	obs.RecordTokenUsage(ctx, 100, "input", "gpt-4")
	obs.RecordTokenUsage(ctx, 50, "output", "gpt-4")
	obs.RecordOperationDuration(ctx, 2.5, "gpt-4", "sess1", "")
	obs.RecordTTFT(ctx, 0.3, "gpt-4")
	obs.RecordHookDuration(ctx, 0.01, "pre_tool_use")

	// Collect and verify.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	metrics := make(map[string]bool, 16)
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			metrics[m.Name] = true
		}
	}

	assert.True(t, metrics["openrouter.http_requests_total"], "http_requests_total")
	assert.True(t, metrics["openrouter.rate_limit_events_total"], "rate_limit_events_total")
	assert.True(t, metrics["openrouter.tool_calls_total"], "tool_calls_total")
	assert.True(t, metrics["openrouter.tool_call_duration"], "tool_call_duration")
	assert.True(t, metrics["openrouter.checkpoint_operations_total"], "checkpoint_operations_total")
	assert.True(t, metrics["gen_ai.client.token.usage"], "token_usage")
	assert.True(t, metrics["gen_ai.client.operation.duration"], "operation_duration")
	assert.True(t, metrics["gen_ai.client.time_to_first_token"], "ttft")
	assert.True(t, metrics["openrouter.hook_duration"], "hook_duration")
}

func TestSpansCreatedWithRealProvider(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	obs := New(Config{TracerProvider: tp})
	ctx := context.Background()

	// Create and end spans.
	ctx, querySpan := obs.StartQuerySpan(ctx, "gpt-4", "sess1")
	_, httpSpan := obs.StartHTTPSpan(ctx, "POST", "/chat/completions")
	httpSpan.End()
	_, toolSpan := obs.StartToolSpan(ctx, "my_tool")
	toolSpan.End()
	_, hookSpan := obs.StartHookSpan(ctx, "pre_tool_use")
	hookSpan.End()
	querySpan.End()

	spans := spanRecorder.Ended()
	require.Len(t, spans, 4)

	// Check span names.
	names := make([]string, 0, len(spans))
	for _, s := range spans {
		names = append(names, s.Name())
	}
	assert.Contains(t, names, "gen_ai.client.operation")
	assert.Contains(t, names, "openrouter.http.request")
	assert.Contains(t, names, "openrouter.tool.call")
	assert.Contains(t, names, "openrouter.hook.dispatch")

	// Verify child spans have the query span as parent.
	for _, s := range spans {
		if s.Name() == "gen_ai.client.operation" {
			continue
		}
		assert.Equal(t, querySpan.SpanContext().TraceID(), s.SpanContext().TraceID(),
			"span %s should share trace ID with parent", s.Name())
	}
}

func TestSpanAttributes(t *testing.T) {
	spanRecorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spanRecorder))
	defer func() { _ = tp.Shutdown(context.Background()) }()

	obs := New(Config{TracerProvider: tp})
	ctx := context.Background()

	_, span := obs.StartQuerySpan(ctx, "claude-3.5-sonnet", "test-sess")
	span.End()

	spans := spanRecorder.Ended()
	require.Len(t, spans, 1)

	attrMap := make(map[string]string, 4)
	for _, kv := range spans[0].Attributes() {
		attrMap[string(kv.Key)] = kv.Value.Emit()
	}

	assert.Equal(t, "query", attrMap["gen_ai.operation.name"])
	assert.Equal(t, "claude-3.5-sonnet", attrMap["gen_ai.request.model"])
	assert.Equal(t, "test-sess", attrMap["gen_ai.session.id"])
}

func TestTokenUsageZeroSkipped(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	obs := New(Config{MeterProvider: mp})
	ctx := context.Background()

	// Record zero tokens — should be skipped.
	obs.RecordTokenUsage(ctx, 0, "input", "model")
	obs.RecordTokenUsage(ctx, -5, "output", "model")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	// Token usage counter should not have any data points.
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == "gen_ai.client.token.usage" {
				if sum, ok := m.Data.(metricdata.Sum[int64]); ok {
					assert.Empty(t, sum.DataPoints, "should have no data points for zero token usage")
				}
			}
		}
	}
}

func TestMetricAttributes(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()

	obs := New(Config{MeterProvider: mp})
	ctx := context.Background()

	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordHTTPRequest(ctx, "4xx", true)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "openrouter.http_requests_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			require.Len(t, sum.DataPoints, 2)

			for _, dp := range sum.DataPoints {
				attrs := dp.Attributes
				statusClass, ok := attrs.Value(attribute.Key("http.response.status_class"))
				assert.True(t, ok, "should have status_class attribute")

				retry, ok := attrs.Value(attribute.Key("http.request.retry"))
				assert.True(t, ok, "should have retry attribute")

				if statusClass.AsString() == "2xx" {
					assert.False(t, retry.AsBool())
				} else {
					assert.True(t, retry.AsBool())
				}
			}
		}
	}
}
