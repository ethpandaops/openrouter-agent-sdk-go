// Package observability provides OpenTelemetry metrics and tracing for the SDK.
//
// When no MeterProvider or TracerProvider is configured, all recording
// operations are true no-ops with zero allocations on the hot path.
package observability

import (
	"context"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
)

const (
	// instrumentationName is the OTel instrumentation scope name.
	instrumentationName = "github.com/ethpandaops/openrouter-agent-sdk-go"
	// instrumentationVersion tracks the SDK version for OTel scope metadata.
	instrumentationVersion = "0.1.0"
)

// Semantic convention attribute keys.
var (
	attrModel             = attribute.Key("gen_ai.request.model")
	attrSessionID         = attribute.Key("gen_ai.session.id")
	attrOperation         = attribute.Key("gen_ai.operation.name")
	attrTokenType         = attribute.Key("gen_ai.token.type")
	attrErrorType         = attribute.Key("error.type")
	attrToolName          = attribute.Key("openrouter.tool.name")
	attrToolOutcome       = attribute.Key("openrouter.tool.outcome")
	attrStatusClass       = attribute.Key("http.response.status_class")
	attrRetry             = attribute.Key("http.request.retry")
	attrHookEvent         = attribute.Key("openrouter.hook.event")
	attrCheckpointOp      = attribute.Key("openrouter.checkpoint.op")
	attrCheckpointOutcome = attribute.Key("openrouter.checkpoint.outcome")
)

// Observer holds pre-created OTel instruments for SDK-wide telemetry.
// All instruments are created once during construction; recording calls
// on the hot path use these pre-created handles.
type Observer struct {
	meter  metric.Meter
	tracer trace.Tracer

	// GenAI semantic convention metrics.
	opDuration metric.Float64Histogram
	tokenUsage metric.Int64Counter
	ttft       metric.Float64Histogram

	// SDK-specific metrics.
	httpRequestsTotal    metric.Int64Counter
	httpRequestDuration  metric.Float64Histogram
	toolCallsTotal       metric.Int64Counter
	toolCallDuration     metric.Float64Histogram
	checkpointOpsTotal   metric.Int64Counter
	rateLimitEventsTotal metric.Int64Counter
	hookDuration         metric.Float64Histogram
}

// Config holds the provider references needed to create an Observer.
type Config struct {
	MeterProvider  metric.MeterProvider
	TracerProvider trace.TracerProvider
}

// New creates an Observer from the given config.
// If both providers are nil, the returned observer uses no-op implementations
// and incurs zero overhead on the recording path.
func New(cfg Config) *Observer {
	mp := cfg.MeterProvider
	if mp == nil {
		mp = noopmetric.NewMeterProvider()
	}

	tp := cfg.TracerProvider
	if tp == nil {
		tp = nooptrace.NewTracerProvider()
	}

	meter := mp.Meter(
		instrumentationName,
		metric.WithInstrumentationVersion(instrumentationVersion),
	)
	tracer := tp.Tracer(
		instrumentationName,
		trace.WithInstrumentationVersion(instrumentationVersion),
	)

	obs := &Observer{
		meter:  meter,
		tracer: tracer,
	}
	obs.initMetrics()

	return obs
}

// Noop returns an observer that performs no recording. This is the default
// when no provider is configured.
func Noop() *Observer {
	return New(Config{})
}

// --- Metric recording helpers ---

// RecordOperationDuration records a gen_ai.client.operation.duration measurement.
// Note: sessionID is intentionally excluded from metric attributes to avoid
// unbounded cardinality. It is included on span attributes where high
// cardinality is expected and manageable.
func (o *Observer) RecordOperationDuration(
	ctx context.Context, seconds float64, model, errType string,
) {
	attrs := []attribute.KeyValue{
		attrOperation.String("query"),
		attrModel.String(model),
	}
	if errType != "" {
		attrs = append(attrs, attrErrorType.String(errType))
	}

	o.opDuration.Record(ctx, seconds, metric.WithAttributes(attrs...))
}

// RecordTokenUsage records gen_ai.client.token.usage for a given token type.
func (o *Observer) RecordTokenUsage(
	ctx context.Context, tokens int64, tokenType, model string,
) {
	if tokens <= 0 {
		return
	}

	o.tokenUsage.Add(ctx, tokens, metric.WithAttributes(
		attrTokenType.String(tokenType),
		attrModel.String(model),
	))
}

// RecordTTFT records gen_ai.client.time_to_first_token_seconds.
func (o *Observer) RecordTTFT(ctx context.Context, seconds float64, model string) {
	o.ttft.Record(ctx, seconds, metric.WithAttributes(
		attrModel.String(model),
	))
}

// RecordHTTPRequest records an openrouter.http_requests_total increment.
func (o *Observer) RecordHTTPRequest(
	ctx context.Context, statusClass string, isRetry bool,
) {
	o.httpRequestsTotal.Add(ctx, 1, metric.WithAttributes(
		attrStatusClass.String(statusClass),
		attrRetry.Bool(isRetry),
	))
}

// RecordHTTPRequestDuration records openrouter.http_request_duration.
func (o *Observer) RecordHTTPRequestDuration(
	ctx context.Context, seconds float64, statusClass string, isRetry bool,
) {
	o.httpRequestDuration.Record(ctx, seconds, metric.WithAttributes(
		attrStatusClass.String(statusClass),
		attrRetry.Bool(isRetry),
	))
}

// RecordRateLimitEvent records an openrouter.rate_limit_events_total increment.
func (o *Observer) RecordRateLimitEvent(ctx context.Context) {
	o.rateLimitEventsTotal.Add(ctx, 1)
}

// RecordToolCall records openrouter.tool_calls_total with outcome.
func (o *Observer) RecordToolCall(
	ctx context.Context, toolName, outcome string,
) {
	o.toolCallsTotal.Add(ctx, 1, metric.WithAttributes(
		attrToolName.String(toolName),
		attrToolOutcome.String(outcome),
	))
}

// RecordToolCallDuration records openrouter.tool_call_duration_seconds.
func (o *Observer) RecordToolCallDuration(
	ctx context.Context, seconds float64, toolName string,
) {
	o.toolCallDuration.Record(ctx, seconds, metric.WithAttributes(
		attrToolName.String(toolName),
	))
}

// RecordCheckpointOp records openrouter.checkpoint_operations_total.
func (o *Observer) RecordCheckpointOp(
	ctx context.Context, op, outcome string,
) {
	o.checkpointOpsTotal.Add(ctx, 1, metric.WithAttributes(
		attrCheckpointOp.String(op),
		attrCheckpointOutcome.String(outcome),
	))
}

// RecordHookDuration records hook execution duration per event type.
func (o *Observer) RecordHookDuration(
	ctx context.Context, seconds float64, event string,
) {
	o.hookDuration.Record(ctx, seconds, metric.WithAttributes(
		attrHookEvent.String(event),
	))
}

// --- Span creation helpers ---

// StartQuerySpan starts a parent span for a conversation turn.
func (o *Observer) StartQuerySpan(
	ctx context.Context, model, sessionID string,
) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, "gen_ai.client.operation",
		trace.WithAttributes(
			attrOperation.String("query"),
			attrModel.String(model),
			attrSessionID.String(sessionID),
		),
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

// StartHTTPSpan starts a child span for an HTTP request.
func (o *Observer) StartHTTPSpan(
	ctx context.Context, method, endpoint string,
) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, "openrouter.http.request",
		trace.WithAttributes(
			attribute.String("http.request.method", method),
			attribute.String("url.path", endpoint),
		),
		trace.WithSpanKind(trace.SpanKindClient),
	)
}

// StartToolSpan starts a child span for a tool invocation.
func (o *Observer) StartToolSpan(
	ctx context.Context, toolName string,
) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, "openrouter.tool.call",
		trace.WithAttributes(
			attrToolName.String(toolName),
		),
	)
}

// StartHookSpan starts a child span for hook dispatch.
func (o *Observer) StartHookSpan(
	ctx context.Context, event string,
) (context.Context, trace.Span) {
	return o.tracer.Start(ctx, "openrouter.hook.dispatch",
		trace.WithAttributes(
			attrHookEvent.String(event),
		),
	)
}

// StatusClass returns the HTTP status class string (e.g., "2xx", "4xx").
func StatusClass(code int) string {
	switch {
	case code >= 200 && code < 300:
		return "2xx"
	case code >= 300 && code < 400:
		return "3xx"
	case code >= 400 && code < 500:
		return "4xx"
	case code >= 500 && code < 600:
		return "5xx"
	default:
		return "other"
	}
}
