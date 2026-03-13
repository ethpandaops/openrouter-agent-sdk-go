//go:build integration

package integration_test

import (
	"context"
	"sync/atomic"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestHooks(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	tool := openroutersdk.NewTool("echo", "Echo text", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		return map[string]any{"echo": input["text"]}, nil
	})

	var preCount atomic.Int32
	var postCount atomic.Int32
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__echo"},
		}),
		openroutersdk.WithHooks(map[openroutersdk.HookEvent][]*openroutersdk.HookMatcher{
			openroutersdk.HookEventPreToolUse: {{
				Hooks: []openroutersdk.HookCallback{
					func(context.Context, openroutersdk.HookInput, *string, *openroutersdk.HookContext) (openroutersdk.HookJSONOutput, error) {
						preCount.Add(1)
						return &openroutersdk.SyncHookJSONOutput{}, nil
					},
				},
			}},
			openroutersdk.HookEventPostToolUse: {{
				Hooks: []openroutersdk.HookCallback{
					func(context.Context, openroutersdk.HookInput, *string, *openroutersdk.HookContext) (openroutersdk.HookJSONOutput, error) {
						postCount.Add(1)
						return &openroutersdk.SyncHookJSONOutput{}, nil
					},
				},
			}},
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText(`Call the echo tool with {"text":"hooked"}.`)); err != nil {
		t.Fatalf("query: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range client.ReceiveMessages(ctx) {
		}
	}()

	if err := waitForCondition(ctx, func() bool { return postCount.Load() > 0 }); err != nil {
		t.Fatalf("wait for hook execution: %v", err)
	}
	_ = client.Interrupt(ctx)
	<-done

	if preCount.Load() == 0 || postCount.Load() == 0 {
		t.Fatalf("expected pre/post hooks to fire, got pre=%d post=%d", preCount.Load(), postCount.Load())
	}
}
