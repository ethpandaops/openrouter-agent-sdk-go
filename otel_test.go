package openroutersdk

import (
	"context"
	"testing"

	"github.com/ethpandaops/agent-sdk-observability/testkit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
)

const testModel = "openai/gpt-4o-mini"

const (
	metricTokenUsage        = "gen_ai.client.token.usage"
	metricOperationDuration = "gen_ai.client.operation.duration"
	metricTTFT              = "gen_ai.client.operation.time_to_first_chunk"
)

func TestNewOTelRecorder_NilProviders(t *testing.T) {
	t.Parallel()

	recorder := newOTelRecorder(nil, nil, nil)
	assert.Nil(t, recorder, "recorder should be nil when both providers are nil")
}

func TestNewOTelRecorder_WithMeterProvider(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder, "recorder should not be nil with meter provider")
	require.NotNil(t, recorder.obs, "observer should be initialized")
}

func TestNewOTelRecorder_WithTracerProvider(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	recorder := newOTelRecorder(nil, traces.Provider(), nil)
	require.NotNil(t, recorder, "recorder should not be nil with tracer provider")
	require.NotNil(t, recorder.obs, "observer should be initialized")
}

func TestOTelRecorder_ObserveNilSafe(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	var recorder *otelRecorder

	recorder.Observe(ctx, nil)
	recorder.Observe(ctx, &message.ResultMessage{})
}

func TestOTelRecorder_ObserveResultMessage(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder)

	result := &message.ResultMessage{
		DurationMs: 1500,
		NumTurns:   3,
		Usage: &message.Usage{
			InputTokens:           100,
			OutputTokens:          50,
			CachedInputTokens:     20,
			ReasoningOutputTokens: 10,
		},
	}

	recorder.Observe(context.Background(), result)

	names := metricNames(t, metrics)
	assert.Contains(t, names, metricTokenUsage)
	assert.Contains(t, names, metricOperationDuration)
}

func TestOTelRecorder_ObserveResultWithError(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder)

	errMsg := "rate limit exceeded"
	result := &message.ResultMessage{
		DurationMs: 500,
		IsError:    true,
		Result:     &errMsg,
		Usage: &message.Usage{
			InputTokens: 50,
		},
	}

	recorder.Observe(context.Background(), result)

	points, err := metrics.HistogramPoints(context.Background(), metricOperationDuration)
	require.NoError(t, err)
	require.NotEmpty(t, points, "duration histogram should have a point")

	var sawRateLimited bool

	for _, p := range points {
		if p.Attributes["error.type"] == "rate_limited" {
			sawRateLimited = true

			break
		}
	}

	assert.True(t, sawRateLimited, "error.type should be rate_limited when result mentions rate limit")
}

func TestOTelRecorder_ObserveResultDurationIncludesModelLabel(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder)

	// Cache the model via an assistant message first.
	recorder.Observe(context.Background(), &message.AssistantMessage{
		Model: testModel,
		Content: []message.ContentBlock{
			&message.TextBlock{Type: message.BlockTypeText, Text: "hello"},
		},
	})

	result := &message.ResultMessage{
		DurationMs: 2000,
		Usage: &message.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	recorder.Observe(context.Background(), result)

	points, err := metrics.HistogramPoints(context.Background(), metricOperationDuration)
	require.NoError(t, err)
	require.NotEmpty(t, points)

	var sawModel bool

	for _, p := range points {
		if p.Attributes["gen_ai.request.model"] == testModel {
			sawModel = true

			break
		}
	}

	assert.True(t, sawModel, "duration metric should include gen_ai.request.model attribute")
}

func TestOTelRecorder_TTFTRecordedOnFirstAssistant(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder)

	recorder.markQueryStart()

	ctx := context.Background()
	recorder.Observe(ctx, &message.AssistantMessage{
		Model: testModel,
		Content: []message.ContentBlock{
			&message.TextBlock{Type: message.BlockTypeText, Text: "hello"},
		},
	})
	// Second assistant message should not record a second TTFT.
	recorder.Observe(ctx, &message.AssistantMessage{
		Model: testModel,
		Content: []message.ContentBlock{
			&message.TextBlock{Type: message.BlockTypeText, Text: "world"},
		},
	})

	points, err := metrics.HistogramPoints(ctx, metricTTFT)
	require.NoError(t, err)
	require.Len(t, points, 1, "TTFT should be recorded exactly once")
	assert.Equal(t, testModel, points[0].Attributes["gen_ai.request.model"])
}

func TestOTelRecorder_TTFTNotRecordedForEmptyAssistant(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)
	require.NotNil(t, recorder)

	recorder.markQueryStart()

	// AssistantMessage with no content blocks should not trigger TTFT.
	recorder.Observe(context.Background(), &message.AssistantMessage{
		Model:   testModel,
		Content: nil,
	})

	points, err := metrics.HistogramPoints(context.Background(), metricTTFT)
	require.NoError(t, err)
	assert.Empty(t, points, "TTFT should not be recorded for empty assistant message")
}

func TestInitMetricsRecorder(t *testing.T) {
	t.Parallel()

	t.Run("nil options", func(t *testing.T) {
		t.Parallel()

		initMetricsRecorder(nil)
	})

	t.Run("no providers", func(t *testing.T) {
		t.Parallel()

		options := &config.Options{}
		initMetricsRecorder(options)
		assert.Nil(t, options.MetricsRecorder)
	})

	t.Run("with meter provider", func(t *testing.T) {
		t.Parallel()

		metrics := testkit.NewMetricsHarness()

		t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

		options := &config.Options{MeterProvider: metrics.Provider()}
		initMetricsRecorder(options)
		assert.NotNil(t, options.MetricsRecorder)
		assert.NotNil(t, options.Observer)
	})

	t.Run("already set", func(t *testing.T) {
		t.Parallel()

		metrics := testkit.NewMetricsHarness()

		t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

		existing := newOTelRecorder(metrics.Provider(), nil, nil)
		options := &config.Options{
			MeterProvider:   metrics.Provider(),
			MetricsRecorder: existing,
		}
		initMetricsRecorder(options)
		assert.Equal(t, existing, options.MetricsRecorder)
	})
}

func TestStartQuerySpan_NoTracer(t *testing.T) {
	t.Parallel()

	options := &config.Options{}

	ctx := context.Background()
	newCtx, span := startQuerySpan(ctx, options, "query")

	assert.Equal(t, ctx, newCtx)
	assert.False(t, span.IsRecording())

	span.End()
}

func TestStartQuerySpan_WithTracer(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	options := &config.Options{
		TracerProvider: traces.Provider(),
		Model:          testModel,
	}

	initMetricsRecorder(options)

	ctx := context.Background()
	_, span := startQuerySpan(ctx, options, "query")
	span.End()

	summaries := traces.Summaries()
	require.Len(t, summaries, 1)
	assert.Equal(t, "chat openai/gpt-4o-mini", summaries[0].Name)
	assert.Equal(t, testModel, summaries[0].Attributes["gen_ai.request.model"])
	assert.Equal(t, "chat", summaries[0].Attributes["gen_ai.operation.name"])
	assert.Equal(t, "openrouter", summaries[0].Attributes["gen_ai.provider.name"])
}

func TestWithMeterProviderOption(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	options := applyAgentOptions([]Option{WithMeterProvider(metrics.Provider())})
	assert.Equal(t, metrics.Provider(), options.MeterProvider)
}

func TestWithTracerProviderOption(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	options := applyAgentOptions([]Option{WithTracerProvider(traces.Provider())})
	assert.Equal(t, traces.Provider(), options.TracerProvider)
}

func TestSessionMetricsRecorderInterface(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	recorder := newOTelRecorder(metrics.Provider(), nil, nil)

	var iface config.SessionMetricsRecorder = recorder
	assert.NotNil(t, iface)

	iface.Observe(context.Background(), &message.ResultMessage{
		Usage: &message.Usage{InputTokens: 10, OutputTokens: 5},
	})
}

func TestApplyAgentOptionsToConfig_InitializesRecorder(t *testing.T) {
	t.Parallel()

	metrics := testkit.NewMetricsHarness()

	t.Cleanup(func() { _ = metrics.Shutdown(context.Background()) })

	options := applyAgentOptionsToConfig([]Option{
		WithMeterProvider(metrics.Provider()),
	})

	require.NotNil(t, options)
	assert.NotNil(t, options.MetricsRecorder)
	assert.NotNil(t, options.Observer)
}

// TestExemplarsAttachedToHistograms verifies that histogram points recorded
// inside a sampled trace context carry trace-based exemplars. The shared
// promexporter sets exemplar.TraceBasedFilter on the underlying MeterProvider,
// so this test guards the metrics-to-traces link against regressions in the
// exemplar-filter wiring.
func TestExemplarsAttachedToHistograms(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithExemplarFilter(exemplar.TraceBasedFilter),
	)

	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })

	tp := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.AlwaysSample()))

	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })

	options := &config.Options{
		MeterProvider:  mp,
		TracerProvider: tp,
		Model:          testModel,
	}
	initMetricsRecorder(options)

	ctx, span := startQuerySpan(context.Background(), options, "query")
	wantTraceID := span.SpanContext().TraceID()
	wantSpanID := span.SpanContext().SpanID()

	options.MetricsRecorder.Observe(ctx, &message.ResultMessage{
		DurationMs: 420,
		Usage: &message.Usage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	})

	span.End()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	var (
		sawHistogram bool
		sawExemplar  bool
		gotTraceID   []byte
		gotSpanID    []byte
	)

	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != metricOperationDuration {
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

	require.True(t, sawHistogram, "expected operation-duration histogram")
	require.True(t, sawExemplar, "expected at least one exemplar on the histogram")
	assert.Equal(t, wantTraceID[:], gotTraceID)
	assert.Equal(t, wantSpanID[:], gotSpanID)
}

func TestOTelRecorder_ObserveResultEnrichesSpan(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	options := &config.Options{
		TracerProvider: traces.Provider(),
		Model:          testModel,
	}
	initMetricsRecorder(options)

	stopReason := "end_turn"
	result := &message.ResultMessage{
		DurationMs: 1000,
		StopReason: &stopReason,
		Usage: &message.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	// Cache model via assistant message.
	ctx, span := startQuerySpan(context.Background(), options, "query")
	options.MetricsRecorder.Observe(ctx, &message.AssistantMessage{
		Model: testModel,
		Content: []message.ContentBlock{
			&message.TextBlock{Type: message.BlockTypeText, Text: "hi"},
		},
	})
	options.MetricsRecorder.Observe(ctx, result)

	span.End()

	summaries := traces.Summaries()
	require.Len(t, summaries, 1)
	assert.Equal(t, testModel, summaries[0].Attributes["gen_ai.response.model"])
	assert.Equal(t, "[\"end_turn\"]", summaries[0].Attributes["gen_ai.response.finish_reasons"])
}

func TestOTelRecorder_ObserveResultSetsSpanErrorStatus(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	options := &config.Options{
		TracerProvider: traces.Provider(),
		Model:          testModel,
	}
	initMetricsRecorder(options)

	errMsg := "rate limit exceeded"
	result := &message.ResultMessage{
		DurationMs: 500,
		IsError:    true,
		Result:     &errMsg,
		Usage: &message.Usage{
			InputTokens: 50,
		},
	}

	ctx, span := startQuerySpan(context.Background(), options, "query")
	options.MetricsRecorder.Observe(ctx, result)

	span.End()

	ended := traces.Ended()
	require.Len(t, ended, 1)

	assert.Equal(t, codes.Error, ended[0].Status().Code)
	assert.Equal(t, "rate_limited", ended[0].Status().Description)

	// Verify error.type attribute is set on the span.
	var sawErrorType bool

	for _, attr := range ended[0].Attributes() {
		if string(attr.Key) == "error.type" && attr.Value.AsString() == "rate_limited" {
			sawErrorType = true

			break
		}
	}

	assert.True(t, sawErrorType, "span should have error.type=rate_limited attribute")
}

func TestOTelRecorder_ObserveResultNoErrorStatusOnSuccess(t *testing.T) {
	t.Parallel()

	traces := testkit.NewTracesHarness()

	t.Cleanup(func() { _ = traces.Shutdown(context.Background()) })

	options := &config.Options{
		TracerProvider: traces.Provider(),
		Model:          testModel,
	}
	initMetricsRecorder(options)

	result := &message.ResultMessage{
		DurationMs: 1000,
		Usage: &message.Usage{
			InputTokens:  100,
			OutputTokens: 50,
		},
	}

	ctx, span := startQuerySpan(context.Background(), options, "query")
	options.MetricsRecorder.Observe(ctx, result)

	span.End()

	ended := traces.Ended()
	require.Len(t, ended, 1)

	// Span status should be unset (not Error, not Ok) per GenAI spec.
	assert.Equal(t, codes.Unset, ended[0].Status().Code,
		"successful result should not set span status")
}

// metricNames returns the set of metric names observed by the harness.
func metricNames(t *testing.T, metrics *testkit.MetricsHarness) map[string]bool {
	t.Helper()

	names, err := metrics.MetricNames(context.Background())
	require.NoError(t, err)

	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}

	return set
}
