//go:build integration

package integration_test

import (
	"context"
	"sync/atomic"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestToolPermissions_AllowExplicit(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var executions atomic.Int32
	tool := openroutersdk.NewTool("allowed_echo", "Echo text", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		executions.Add(1)
		return map[string]any{"echo": input["text"]}, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__allowed_echo"},
		}),
		openroutersdk.WithCanUseTool(func(_ context.Context, _ string, _ map[string]any, _ *openroutersdk.ToolPermissionContext) (openroutersdk.PermissionResult, error) {
			return &openroutersdk.PermissionResultAllow{}, nil
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText(`Call allowed_echo with {"text":"permitted"}.`)); err != nil {
		t.Fatalf("query: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range client.ReceiveMessages(ctx) {
		}
	}()

	if err := waitForCondition(ctx, func() bool { return executions.Load() > 0 }); err != nil {
		t.Fatalf("wait for tool execution: %v", err)
	}
	_ = client.Interrupt(ctx)
	<-done

	if executions.Load() == 0 {
		t.Fatal("expected tool to execute with explicit allow")
	}
}

func TestToolPermissions_DenyWithInterrupt(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	tool := openroutersdk.NewTool("denied_echo", "Echo text", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}, func(_ context.Context, _ map[string]any) (map[string]any, error) {
		t.Fatal("tool should not execute when denied")
		return nil, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__denied_echo"},
		}),
		openroutersdk.WithCanUseTool(func(_ context.Context, _ string, _ map[string]any, _ *openroutersdk.ToolPermissionContext) (openroutersdk.PermissionResult, error) {
			return &openroutersdk.PermissionResultDeny{
				Behavior:  "deny",
				Message:   "not allowed",
				Interrupt: true,
			}, nil
		}),
	)

	var sawErrorResult bool
	for msg, err := range openroutersdk.Query(ctx, openrouterText(`Call denied_echo with {"text":"blocked"}.`), opts...) {
		if err != nil {
			// Error propagation is also acceptable for deny+interrupt.
			sawErrorResult = true
			break
		}
		if result, ok := msg.(*openroutersdk.ResultMessage); ok {
			if result.IsError {
				sawErrorResult = true
			}
		}
	}

	if !sawErrorResult {
		t.Fatal("expected error result from permission denial with interrupt")
	}
}

func TestToolPermissions_DisallowedTools(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var executions atomic.Int32
	tool := openroutersdk.NewTool("blocked_tool", "Should not execute", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, func(_ context.Context, _ map[string]any) (map[string]any, error) {
		executions.Add(1)
		return map[string]any{"value": "blocked"}, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithDisallowedTools("mcp__sdk__blocked_tool"),
	)

	// Query and consume — the tool should never execute due to disallowed list.
	for _, err := range openroutersdk.Query(ctx, openrouterText("Call the blocked_tool tool."), opts...) {
		if err != nil {
			break
		}
	}

	if executions.Load() > 0 {
		t.Fatal("expected disallowed tool to not execute")
	}
}

func TestToolPermissions_ClientInteractive(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var callbackInvoked atomic.Bool
	tool := openroutersdk.NewTool("interactive_echo", "Echo text", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		return map[string]any{"echo": input["text"]}, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__interactive_echo"},
		}),
		openroutersdk.WithCanUseTool(func(_ context.Context, name string, _ map[string]any, _ *openroutersdk.ToolPermissionContext) (openroutersdk.PermissionResult, error) {
			callbackInvoked.Store(true)
			return &openroutersdk.PermissionResultAllow{}, nil
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText(`Call interactive_echo with {"text":"interactive"}.`)); err != nil {
		t.Fatalf("query: %v", err)
	}

	var (
		sawAssistant bool
		sawResult    bool
	)

	for msg, err := range client.ReceiveResponse(ctx) {
		if err != nil {
			break
		}

		switch msg.(type) {
		case *openroutersdk.AssistantMessage:
			sawAssistant = true
		case *openroutersdk.ResultMessage:
			sawResult = true
		}
	}

	if !callbackInvoked.Load() {
		t.Fatal("expected permission callback to be invoked")
	}
	if !sawAssistant && !sawResult {
		t.Fatal("expected at least one message type")
	}
}
