# openrouter-agent-sdk-go

`openrouter-agent-sdk-go` is a stateful Go SDK for OpenRouter with sibling ergonomics to the Claude and Codex SDKs, but with an OpenRouter-native public identity.

This is a full cutover:

- package name: `openroutersdk`
- root option type: `OpenRouterAgentOptions`
- canonical transport hook: `WithTransport(...)`
- no exported Claude or CLI compatibility aliases

## Install

```bash
go get github.com/ethpandaops/openrouter-agent-sdk-go
```

## Developer Workflow

The repo ships a sibling-style `Makefile`:

- `make test` runs race-enabled package tests with coverage output.
- `make test-integration` runs `./integration/...` with `-tags=integration`.
- `make audit` runs the aggregate quality gate.

Integration setup:

- `OPENROUTER_API_KEY` is required.
- `OPENROUTER_MODEL` is optional and defaults to `openrouter/free`.
- Use the free router for routine smoke runs; override `OPENROUTER_MODEL` with a pinned tool-capable model when you need stricter capability coverage.
- integration tests are written to skip cleanly when the API key is absent.

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"time"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for msg, err := range openroutersdk.Query(
		ctx,
		openroutersdk.Text("Write a two-line haiku about Go concurrency."),
		openroutersdk.WithAPIKey("..."),
		openroutersdk.WithModel("openrouter/free"),
	) {
		if err != nil {
			panic(err)
		}

		if result, ok := msg.(*openroutersdk.ResultMessage); ok && result.Result != nil {
			fmt.Println(*result.Result)
		}
	}
}
```

## Surface

- `Query(ctx, content, ...opts)` and `QueryStream(...)` return `iter.Seq2[Message, error]`.
- `NewClient()` exposes `Start`, `StartWithContent`, `StartWithStream`, `Query`, `ReceiveMessages`, `ReceiveResponse`, `Interrupt`, `SetPermissionMode`, `SetModel`, `ListModels`, `ListModelsResponse`, `GetMCPStatus`, `RewindFiles`, and `Close`.
- Unsupported peer-parity controls such as `ReconnectMCPServer`, `ToggleMCPServer`, `StopTask`, and `SendToolResult` are present on `Client` and return typed `UnsupportedControlError`s.
- `UserMessageContent` is the canonical input shape. Use `Text(...)` for text-only calls and `Blocks(...)` with `ImageInput(...)`, `FileInput(...)`, `AudioInput(...)`, or `VideoInput(...)` for multimodal chat-completions requests.
- `WithSDKTools(...)` registers high-level in-process tools under `mcp__sdk__<name>`.
- `WithOnUserInput(...)` handles SDK-owned user-input prompts built on top of tool calling.
- `ListModels(...)` and `ListModelsResponse(...)` use OpenRouter model discovery.
- `StatSession(...)`, `ListSessions(...)`, and `GetSessionMessages(...)` operate on the SDK's local persisted session store.

## Model Discovery

- Authenticated discovery defaults to `/api/v1/models/user`.
- Fallback catalog discovery uses `/api/v1/models`.
- `ModelInfo` exposes helper methods such as `CostTier()`, `SupportsToolCalling()`, `SupportsStructuredOutput()`, `SupportsReasoning()`, `SupportsImageInput()`, `SupportsImageOutput()`, `SupportsWebSearch()`, `SupportsPromptCaching()`, `MaxContextLength()`, and parsed pricing helpers.

## Image Output

- Generated images are surfaced as `*ImageBlock` values inside `AssistantMessage.Content`.
- `ImageBlock.Decode()` returns raw bytes plus media type for data-URL-backed images.
- `ImageBlock.Save(path)` writes generated images to disk.
- Live image-generation coverage is available behind the integration build tag when `OPENROUTER_IMAGE_MODEL` is set.

## Multimodal Input

OpenRouter multimodal input in this SDK is block-based and currently targets chat-completions mode.

```go
content := openroutersdk.Blocks(
	openroutersdk.TextInput("Compare these two screenshots and the attached spec file."),
	openroutersdk.ImageInput("https://example.com/before.png"),
	openroutersdk.ImageInput("data:image/png;base64,..."),
	openroutersdk.FileInput("spec.pdf", "data:application/pdf;base64,..."),
)

for msg, err := range openroutersdk.Query(ctx, content,
	openroutersdk.WithAPIKey("..."),
	openroutersdk.WithModel("openai/gpt-4o-mini"),
) {
	_ = msg
	_ = err
}
```

- `ImageInput(...)` accepts a normal URL or a base64 data URL.
- `FileInput(...)` accepts a filename plus `file_data` URL/data URL.
- `AudioInput(...)` accepts base64 audio data plus a format.
- `VideoInput(...)` accepts a normal URL or a data URL.
- The SDK rejects multimodal input in Responses mode with an explicit error instead of faking support.

## Session Semantics

Session APIs are local SDK APIs, not remote OpenRouter account sessions.

- They read from the SDK session store configured with `WithSessionStorePath(...)` or `OPENROUTER_AGENT_SESSION_STORE_PATH`.
- They do not derive from chat `session_id`.
- They do not derive from Responses `previous_response_id`.

## Unsupported Controls

OpenRouter does not have meaningful backend equivalents for some sibling control-plane methods. The SDK exposes those methods where peer parity matters, but they fail explicitly with `UnsupportedControlError` instead of faking semantics.

## Observability

The SDK emits OpenTelemetry metrics and traces. All providers default to noop —
no telemetry is emitted unless you opt in via one of these options:

- `WithMeterProvider(metric.MeterProvider)` — any OTel meter provider
- `WithTracerProvider(trace.TracerProvider)` — any OTel tracer provider
- `WithPrometheusRegisterer(prometheus.Registerer)` — convenience: builds an
  OTel meter provider that exports to your Prometheus registry

### Quick start (Prometheus)

```go
reg := prometheus.NewRegistry()
http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

for msg, err := range sdk.Query(ctx, sdk.Text("hello"),
    sdk.WithModel("openai/gpt-4o-mini"),
    sdk.WithPrometheusRegisterer(reg),
) {
    // ...
}
```

See [`examples/prometheus_metrics`](./examples/prometheus_metrics) for a
runnable end-to-end example.

### Metrics emitted

GenAI spec metrics (cross-SDK comparable, carry `gen_ai.provider.name=openrouter`):

| OTel name                                          | Prometheus scrape                                        |
|----------------------------------------------------|----------------------------------------------------------|
| `gen_ai.client.operation.duration`                 | `gen_ai_client_operation_duration_seconds`               |
| `gen_ai.client.token.usage`                        | `gen_ai_client_token_usage`                              |
| `gen_ai.client.operation.time_to_first_chunk`      | `gen_ai_client_operation_time_to_first_chunk_seconds`    |

OpenRouter-specific metrics:

| OTel name                                  | Labels                          |
|--------------------------------------------|---------------------------------|
| `openrouter.http_requests_total`           | `status_class`, `retry`         |
| `openrouter.http_request_duration`         | `status_class`, `retry`         |
| `openrouter.rate_limit_events_total`       | —                               |
| `openrouter.tool_calls_total`              | `gen_ai.tool.name`, `outcome`   |
| `openrouter.tool_call_duration`            | `gen_ai.tool.name`              |
| `openrouter.checkpoint_operations_total`   | `checkpoint.op`, `outcome`      |
| `openrouter.hook_dispatch_duration`        | `hook.event`, `outcome`         |

### Spans emitted

| Span name                          | Kind     | Notes                                    |
|------------------------------------|----------|------------------------------------------|
| `chat {model}` (GenAI spec format) | CLIENT   | One per query; carries GenAI attributes  |
| `execute_tool {name}` (spec)       | INTERNAL | Per tool call; `gen_ai.tool.call.id` set |
| `openrouter.http.request`          | CLIENT   | Per HTTP request, retry events           |
| `openrouter.hook.dispatch`         | INTERNAL | Per hook event                           |

Duration histograms carry trace exemplars when traces are sampled, so latency
spikes link directly to traces in Grafana / Tempo / Jaeger.

## Examples

Runnable examples live under [`examples`](./examples).
