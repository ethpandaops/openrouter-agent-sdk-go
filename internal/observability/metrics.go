package observability

import "go.opentelemetry.io/otel/metric"

// initMetrics pre-creates all OTel metric instruments.
// Errors from instrument creation are intentionally ignored; the OTel API
// returns noop instruments on failure, maintaining zero-overhead behavior.
func (o *Observer) initMetrics() {
	// GenAI semantic convention metrics.
	o.opDuration, _ = o.meter.Float64Histogram(
		"gen_ai.client.operation.duration",
		metric.WithDescription("Duration of GenAI client operations"),
		metric.WithUnit("s"),
	)

	o.tokenUsage, _ = o.meter.Int64Counter(
		"gen_ai.client.token.usage",
		metric.WithDescription("Number of tokens used by GenAI client operations"),
		metric.WithUnit("{token}"),
	)

	o.ttft, _ = o.meter.Float64Histogram(
		"gen_ai.client.time_to_first_token",
		metric.WithDescription("Time from request start to first token received"),
		metric.WithUnit("s"),
	)

	// SDK-specific metrics.
	o.httpRequestsTotal, _ = o.meter.Int64Counter(
		"openrouter.http_requests_total",
		metric.WithDescription("Total number of HTTP requests to OpenRouter API"),
		metric.WithUnit("{request}"),
	)

	o.httpRequestDuration, _ = o.meter.Float64Histogram(
		"openrouter.http_request_duration",
		metric.WithDescription("Duration of HTTP requests to OpenRouter API"),
		metric.WithUnit("s"),
	)

	o.toolCallsTotal, _ = o.meter.Int64Counter(
		"openrouter.tool_calls_total",
		metric.WithDescription("Total number of tool call invocations"),
		metric.WithUnit("{call}"),
	)

	o.toolCallDuration, _ = o.meter.Float64Histogram(
		"openrouter.tool_call_duration",
		metric.WithDescription("Duration of tool call executions"),
		metric.WithUnit("s"),
	)

	o.checkpointOpsTotal, _ = o.meter.Int64Counter(
		"openrouter.checkpoint_operations_total",
		metric.WithDescription("Total number of checkpoint operations"),
		metric.WithUnit("{operation}"),
	)

	o.rateLimitEventsTotal, _ = o.meter.Int64Counter(
		"openrouter.rate_limit_events_total",
		metric.WithDescription("Total number of rate limit events (HTTP 429)"),
		metric.WithUnit("{event}"),
	)

	o.hookDuration, _ = o.meter.Float64Histogram(
		"openrouter.hook_duration",
		metric.WithDescription("Duration of hook callback executions"),
		metric.WithUnit("s"),
	)
}
