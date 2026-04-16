package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/ethpandaops/agent-sdk-observability/testkit"
	upstreamgenai "go.opentelemetry.io/otel/semconv/v1.40.0/genaiconv"
)

func TestNoopObserverDoesNotPanic(t *testing.T) {
	obs := Noop()
	ctx := context.Background()

	obs.RecordOperationDuration(ctx, 1.5, upstreamgenai.OperationNameChat, "test-model", "")
	obs.RecordTokenUsage(ctx, 100, upstreamgenai.TokenTypeInput, upstreamgenai.OperationNameChat, "test-model")
	obs.RecordTTFT(ctx, 0.5, "test-model")
	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordRateLimitEvent(ctx)
	obs.RecordToolCall(ctx, "tool1", "ok")
	obs.RecordToolCallDuration(ctx, 0.1, "tool1")
	obs.RecordCheckpointOp(ctx, "create", "ok")
	obs.RecordHookDuration(ctx, 0.01, "PreToolUse", "ok")

	_, span := obs.StartQuerySpan(ctx, upstreamgenai.OperationNameChat, "model", "sess")
	span.End()
}

func TestStatusClassOf(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{200, "2xx"},
		{404, "4xx"},
		{429, "4xx"},
		{503, "5xx"},
		{0, "other"},
	}
	for _, tt := range tests {
		if got := StatusClassOf(tt.code); got != tt.want {
			t.Fatalf("StatusClassOf(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func TestMetricsRecordedWithRealProvider(t *testing.T) {
	metrics := testkit.NewMetricsHarness()
	defer func() { _ = metrics.Shutdown(context.Background()) }()

	obs, err := New(Config{MeterProvider: metrics.Provider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	obs.RecordHTTPRequest(ctx, "2xx", false)
	obs.RecordHTTPRequestDuration(ctx, 0.2, "2xx", false)
	obs.RecordRateLimitEvent(ctx)
	obs.RecordToolCall(ctx, "my_tool", "ok")
	obs.RecordToolCallDuration(ctx, 0.5, "my_tool")
	obs.RecordCheckpointOp(ctx, "restore", "no_checkpoint")
	obs.RecordTokenUsage(ctx, 50, upstreamgenai.TokenTypeAttr("thinking"), upstreamgenai.OperationNameChat, "gpt-4")
	obs.RecordOperationDuration(ctx, 2.5, upstreamgenai.OperationNameChat, "gpt-4", "")
	obs.RecordTTFT(ctx, 0.3, "gpt-4")
	obs.RecordHookDuration(ctx, 0.01, "PreToolUse", "ok")

	names, err := metrics.MetricNames(ctx)
	if err != nil {
		t.Fatalf("MetricNames() error = %v", err)
	}
	nameSet := map[string]bool{}
	for _, name := range names {
		nameSet[name] = true
	}

	required := []string{
		"gen_ai.client.operation.duration",
		"gen_ai.client.token.usage",
		"gen_ai.client.operation.time_to_first_chunk",
		"openrouter.http_requests_total",
		"openrouter.http_request_duration",
		"openrouter.tool_calls_total",
		"openrouter.tool_call_duration",
		"openrouter.checkpoint_operations_total",
		"openrouter.rate_limit_events_total",
		"openrouter.hook_dispatch_duration",
	}
	for _, name := range required {
		if !nameSet[name] {
			t.Fatalf("missing metric %q", name)
		}
	}
}

func TestMetricAttributes(t *testing.T) {
	metrics := testkit.NewMetricsHarness()
	defer func() { _ = metrics.Shutdown(context.Background()) }()

	obs, err := New(Config{MeterProvider: metrics.Provider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	obs.RecordHTTPRequest(ctx, "4xx", true)
	obs.RecordHTTPRequestDuration(ctx, 0.05, "4xx", true)
	obs.RecordHookDuration(ctx, 0.05, "PostToolUseFailure", "error")

	httpPoints, err := metrics.Int64Points(ctx, "openrouter.http_requests_total")
	if err != nil {
		t.Fatalf("Int64Points() error = %v", err)
	}
	if len(httpPoints) != 1 {
		t.Fatalf("expected 1 http point, got %d", len(httpPoints))
	}
	if httpPoints[0].Attributes["status_class"] != "4xx" {
		t.Fatalf("unexpected status_class: %q", httpPoints[0].Attributes["status_class"])
	}
	if httpPoints[0].Attributes["retry"] != "true" {
		t.Fatalf("unexpected retry: %q", httpPoints[0].Attributes["retry"])
	}

	httpDurationPoints, err := metrics.HistogramPoints(ctx, "openrouter.http_request_duration")
	if err != nil {
		t.Fatalf("HistogramPoints(http_request_duration) error = %v", err)
	}
	if len(httpDurationPoints) != 1 {
		t.Fatalf("expected 1 http duration point, got %d", len(httpDurationPoints))
	}
	if httpDurationPoints[0].Attributes["status_class"] != "4xx" {
		t.Fatalf("unexpected duration status_class: %q", httpDurationPoints[0].Attributes["status_class"])
	}
	if httpDurationPoints[0].Attributes["retry"] != "true" {
		t.Fatalf("unexpected duration retry: %q", httpDurationPoints[0].Attributes["retry"])
	}

	hookPoints, err := metrics.HistogramPoints(ctx, "openrouter.hook_dispatch_duration")
	if err != nil {
		t.Fatalf("HistogramPoints() error = %v", err)
	}
	if len(hookPoints) != 1 {
		t.Fatalf("expected 1 hook point, got %d", len(hookPoints))
	}
	if hookPoints[0].Attributes["hook.event"] != "PostToolUseFailure" {
		t.Fatalf("unexpected hook.event: %q", hookPoints[0].Attributes["hook.event"])
	}
	if hookPoints[0].Attributes["outcome"] != "error" {
		t.Fatalf("unexpected outcome: %q", hookPoints[0].Attributes["outcome"])
	}
}

func TestSpansCreatedWithRealProvider(t *testing.T) {
	traces := testkit.NewTracesHarness()
	defer func() { _ = traces.Shutdown(context.Background()) }()

	obs, err := New(Config{TracerProvider: traces.Provider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, querySpan := obs.StartQuerySpan(context.Background(), upstreamgenai.OperationNameChat, "gpt-4", "sess1")
	_, httpSpan := obs.StartHTTPSpan(ctx, "/chat/completions")
	httpSpan.AddEvent("retry", RetryAttempt(2), RetryDelay(0))
	httpSpan.End()
	_, toolSpan := obs.StartToolSpan(ctx, "my_tool", "call_42")
	toolSpan.SetAttributes(Outcome("ok"))
	toolSpan.End()
	querySpan.End()

	spans := traces.Summaries()
	if len(spans) != 3 {
		t.Fatalf("expected 3 spans, got %d", len(spans))
	}

	names := map[string]bool{}
	for _, s := range spans {
		names[s.Name] = true
	}
	for _, want := range []string{"chat gpt-4", "execute_tool my_tool", "openrouter.http.request"} {
		if !names[want] {
			t.Fatalf("missing span name %q (got %v)", want, names)
		}
	}
}

func TestSpanEventsAndClassification(t *testing.T) {
	traces := testkit.NewTracesHarness()
	defer func() { _ = traces.Shutdown(context.Background()) }()

	obs, err := New(Config{TracerProvider: traces.Provider()})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, span := obs.StartQuerySpan(context.Background(), upstreamgenai.OperationNameChat, "claude-3.5-sonnet", "test-sess")
	obs.RecordRateLimitEvent(ctx)
	obs.RecordCheckpointOp(ctx, "restore", "no_checkpoint")
	obs.RecordTokenUsage(ctx, 42, upstreamgenai.TokenTypeAttr("thinking"), upstreamgenai.OperationNameChat, "claude-3.5-sonnet")
	span.RecordError(&HTTPStatusError{StatusCode: 429, Body: "slow down"})
	span.End()

	spans := traces.Summaries()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	var sawRateLimit bool
	var sawCheckpoint bool
	var sawThinking bool
	for _, ev := range spans[0].Events {
		switch ev {
		case "rate_limit":
			sawRateLimit = true
		case "checkpoint":
			sawCheckpoint = true
		case "thinking_tokens":
			sawThinking = true
		}
	}
	if !sawRateLimit || !sawCheckpoint || !sawThinking {
		t.Fatalf("missing expected span events: rate_limit=%v checkpoint=%v thinking=%v", sawRateLimit, sawCheckpoint, sawThinking)
	}
}

func TestClassify(t *testing.T) {
	obs, err := New(Config{})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if got := obs.Classify(&HTTPStatusError{StatusCode: 429, Body: "x"}); got != "rate_limited" {
		t.Fatalf("expected rate_limited, got %q", got)
	}
	if got := obs.Classify(errors.New("authentication failed")); got != "auth" {
		t.Fatalf("expected auth, got %q", got)
	}
}
