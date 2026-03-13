//go:build integration

package integration_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestSDKTools_ReturnValue(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var executions atomic.Int32
	var returnedValue string
	tool := openroutersdk.NewTool("get_value", "Return the value 42", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, func(_ context.Context, _ map[string]any) (map[string]any, error) {
		executions.Add(1)
		returnedValue = "42"
		return map[string]any{"value": 42}, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__get_value"},
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText("Call get_value and tell me the result.")); err != nil {
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

	if returnedValue != "42" {
		t.Fatal("expected tool to return 42")
	}
}

func TestSDKTools_MultiTool(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var addExecutions atomic.Int32
	addTool := openroutersdk.NewTool("add", "Add two numbers", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number"},
			"b": map[string]any{"type": "number"},
		},
		"required": []string{"a", "b"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		addExecutions.Add(1)
		a, _ := input["a"].(float64)
		b, _ := input["b"].(float64)
		return map[string]any{"result": a + b}, nil
	})

	var mulExecutions atomic.Int32
	mulTool := openroutersdk.NewTool("multiply", "Multiply two numbers", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"a": map[string]any{"type": "number"},
			"b": map[string]any{"type": "number"},
		},
		"required": []string{"a", "b"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		mulExecutions.Add(1)
		a, _ := input["a"].(float64)
		b, _ := input["b"].(float64)
		return map[string]any{"result": a * b}, nil
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithSDKTools(addTool, mulTool))

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("What is 3+4? Use the add tool."), opts...))
	if result.Result == nil {
		t.Fatal("expected result")
	}

	if addExecutions.Load() == 0 {
		t.Fatal("expected add tool to execute")
	}
}

func TestSDKTools_ErrorHandler(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	tool := openroutersdk.NewTool("failing_tool", "Always fails", map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}, func(_ context.Context, _ map[string]any) (map[string]any, error) {
		return nil, fmt.Errorf("intentional failure")
	})

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithSDKTools(tool),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__failing_tool"},
		}),
	)

	// Should still complete — the error is reported to the model, not to the caller.
	var sawResult bool
	for msg, err := range openroutersdk.Query(ctx, openrouterText("Call failing_tool."), opts...) {
		if err != nil {
			// Error propagation is also acceptable behavior.
			return
		}
		if _, ok := msg.(*openroutersdk.ResultMessage); ok {
			sawResult = true
		}
	}

	if !sawResult {
		t.Fatal("expected result even with failing tool")
	}
}

func TestMCPServer_SDKServer(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var executions atomic.Int32
	addTool := openroutersdk.NewSdkMcpTool("add", "Add two numbers",
		openroutersdk.SimpleSchema(map[string]string{"a": "float64", "b": "float64"}),
		func(_ context.Context, req *openroutersdk.CallToolRequest) (*openroutersdk.CallToolResult, error) {
			args, err := openroutersdk.ParseArguments(req)
			if err != nil {
				return openroutersdk.ErrorResult(err.Error()), nil
			}
			executions.Add(1)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return openroutersdk.TextResult(fmt.Sprintf("%.0f", a+b)), nil
		},
		openroutersdk.WithAnnotations(&openroutersdk.McpToolAnnotations{
			ReadOnlyHint: true,
		}),
	)

	calcServer := openroutersdk.CreateSdkMcpServer("calc", "1.0.0", addTool)

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithMCPServers(map[string]openroutersdk.MCPServerConfig{
			"calc": calcServer,
		}),
		openroutersdk.WithAllowedTools("mcp__calc__add"),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__calc__add"},
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText("What is 3 + 4? Use the add tool.")); err != nil {
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
		t.Fatal("expected MCP calculator tool to execute")
	}
}

func TestMCPStatus(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	addTool := openroutersdk.NewSdkMcpTool("add", "Add", openroutersdk.SimpleSchema(map[string]string{"a": "float64", "b": "float64"}),
		func(_ context.Context, req *openroutersdk.CallToolRequest) (*openroutersdk.CallToolResult, error) {
			args, _ := openroutersdk.ParseArguments(req)
			a, _ := args["a"].(float64)
			b, _ := args["b"].(float64)
			return openroutersdk.TextResult(fmt.Sprintf("%.0f", a+b)), nil
		},
	)
	server := openroutersdk.CreateSdkMcpServer("status_test", "1.0.0", addTool)

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithMCPServers(map[string]openroutersdk.MCPServerConfig{
			"status_test": server,
		}),
		openroutersdk.WithAllowedTools("mcp__status_test__add"),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	status, err := client.GetMCPStatus(ctx)
	if err != nil {
		t.Fatalf("get mcp status: %v", err)
	}

	if status == nil {
		t.Fatal("expected non-nil MCP status")
	}
}
