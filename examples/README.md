# Examples

This directory contains runnable examples for `github.com/ethpandaops/openrouter-agent-sdk-go`.

## API Overview

The SDK supports two primary styles:

1. Top-level one-shot APIs:
- `Query(ctx, content, ...opts)`
- `QueryStream(ctx, messages, ...opts)`

2. Stateful client API:
- `NewClient()` + `Start()`
- `Query()` + `ReceiveResponse()` / `ReceiveMessages()`
- `Interrupt()` / `SetModel()` / `SetPermissionMode()`

## Environment

Required:
- `OPENROUTER_API_KEY`

Optional model overrides:
- `OPENROUTER_MODEL` (default: `openrouter/free`)
- `OPENROUTER_IMAGE_MODEL` (default: `google/gemini-2.5-flash-image`)
- `OPENROUTER_VISION_MODEL` (defaults to `OPENROUTER_MODEL`, then `OPENROUTER_IMAGE_MODEL`)
- `OPENROUTER_IMAGE_OUTPUT_DIR` (optional directory for saving generated images)

## Cost Defaults

Examples intentionally default to low-cost/free models where possible.
Use `OPENROUTER_MODEL` to pin a stricter tool-capable or paid model when you need deterministic capability coverage.

## Core SDK Examples

These focus on the sibling-style SDK contract that downstream code consumes directly.

| Example | Description |
|---|---|
| `quick_start` | Basic one-shot query with low-cost model defaults. |
| `query_stream` | Streaming input via `QueryStream` and `MessagesFromSlice`. |
| `client_multi_turn` | Stateful `Client` usage over multiple turns. |
| `model_discovery` | List models and inspect free/tool/structured-output/image capabilities. |
| `structured_output` | Structured JSON output with `WithOutputFormat`. |
| `sdk_tools` | In-process SDK tools via `WithSDKTools(...)`. |
| `on_user_input` | SDK-owned user input prompts via `WithOnUserInput(...)`. |
| `permissions` | Tool permission denial handling via `WithCanUseTool(...)`. |
| `hooks` | Hook callbacks around tool execution. |
| `sessions_local` | Local session persistence, listing, stats, and message inspection. |
| `interrupt` | Client cancellation via `Interrupt()`. |
| `error_handling` | Typed `UnsupportedControlError` and `ErrSessionNotFound` handling. |
| `system_prompt` | System prompt configuration (default vs custom string vs preset). |
| `extended_thinking` | Extended thinking with `WithThinking` and `WithEffort`. |
| `include_partial_messages` | Real-time streaming of partial message deltas. |
| `max_budget_usd` | API cost control with `WithMaxBudgetUSD`. |
| `cancellation` | Context cancellation and graceful client shutdown. |
| `parallel_queries` | Concurrent `Query()` calls with `errgroup`. |
| `pipeline` | Multi-step LLM orchestration (Generate → Evaluate → Refine). |
| `mcp_calculator` | In-process MCP server with calculator tools via `CreateSdkMcpServer`. |
| `mcp_status` | Query MCP server connection status via `GetMCPStatus`. |
| `memory_tool` | Filesystem-backed persistent memory via MCP tools. |
| `custom_logger` | Bridge any logging library (logrus) to `WithLogger` via `slog.Handler`. |

## OpenRouter-Native Advanced Examples

These focus on OpenRouter-specific routing and request-shape controls.

| Example | Description |
|---|---|
| `openrouter_chat_controls` | Sampling/tool controls (`top_p`, penalties, seed, stop, logprobs). |
| `openrouter_routing` | Provider/plugins/route/session/trace controls. |
| `openrouter_responses` | `/responses` mode with instructions/text config/service tier/truncation. |
| `openrouter_responses_chaining` | Responses chaining with `previous_response_id` and prompt cache key. |
| `openrouter_multimodal_input` | Multimodal chat-completions input with block-based text + image parts. |
| `openrouter_multimodal_image` | Multimodal/image generation with generated image blocks saved to disk. |
| `openrouter_extra` | Escape-hatch payload overrides via `WithOpenRouterExtra`. |

## Running

```bash
# Run any example
go run ./examples/quick_start
go run ./examples/openrouter_responses
go run ./examples/openrouter_multimodal_input

# Examples with sub-examples accept a name argument
go run ./examples/extended_thinking all
go run ./examples/cancellation graceful_shutdown
```

## Testing

```bash
# Run all examples and verify output with OpenRouter
scripts/test_examples.sh

# Run specific examples
scripts/test_examples.sh -f quick_start,pipeline

# Keep going on failure
scripts/test_examples.sh -k

# Override the model if you need stricter capability coverage
OPENROUTER_MODEL=google/gemma-3-4b-it:free scripts/test_examples.sh

# Or force a paid/pinned model explicitly
OPENROUTER_MODEL=openai/gpt-4o-mini scripts/test_examples.sh

# Image-generation example with explicit image model and output directory
OPENROUTER_IMAGE_MODEL=google/gemini-2.5-flash-image \
OPENROUTER_IMAGE_OUTPUT_DIR=/tmp/or-images \
go run ./examples/openrouter_multimodal_image

# Multimodal input example with a vision-capable model
OPENROUTER_VISION_MODEL=openai/gpt-4o-mini \
go run ./examples/openrouter_multimodal_input
```
