//go:build integration

package integration_test

import (
	"context"
	"strings"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestPermissionCallback(t *testing.T) {
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

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type": "function",
			"function": map[string]any{"name": "mcp__sdk__echo"},
		}),
		openroutersdk.WithCanUseTool(func(_ context.Context, name string, input map[string]any, _ *openroutersdk.ToolPermissionContext) (openroutersdk.PermissionResult, error) {
			if name != "mcp__sdk__echo" {
				t.Fatalf("unexpected tool: %s", name)
			}
			return &openroutersdk.PermissionResultDeny{Behavior: "deny", Message: "integration denied"}, nil
		}),
	)

	var gotErr error
	for _, err := range openroutersdk.Query(ctx, openrouterText(`Call the echo tool with {"text":"blocked"}.`), opts...) {
		if err != nil {
			gotErr = err
			break
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "integration denied") {
		t.Fatalf("expected permission denial, got %v", gotErr)
	}
}
