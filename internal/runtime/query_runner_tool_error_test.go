package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/mcp"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
)

// erroringToolServer mimics an MCP SDK server whose CallTool returns a Go
// error (e.g. wagie's execute tool wrapping a SyntaxError). A CallToolResult
// with IsError=true is NOT the same path — this reproduces the Go-error path
// specifically.
type erroringToolServer struct {
	calls int
}

func (s *erroringToolServer) Name() string    { return "srv" }
func (s *erroringToolServer) Version() string { return "1.0.0" }
func (s *erroringToolServer) ListTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "echo",
			"description": "echo",
			"inputSchema": map[string]any{"type": "object"},
		},
	}
}

func (s *erroringToolServer) CallTool(
	_ context.Context,
	_ string,
	_ map[string]any,
) (map[string]any, error) {
	s.calls++
	return nil, fmt.Errorf("running execute code: %w", fmt.Errorf("SyntaxError: unexpected token"))
}

// TestRunMessagesToolErrorSurfacesAsToolResult proves the bug described in
// issue #7: when an MCP tool's CallTool returns a Go error, the SDK should
// feed an is_error tool_result back to the model so it can self-correct —
// not terminate the session by pushing the error onto the errs channel.
//
// With the current implementation (query_runner.go:775-777) this test fails:
//   - the error is forwarded on errs
//   - no ToolResultBlock reaches the model
//   - the transport is only invoked once (no follow-up turn)
func TestRunMessagesToolErrorSurfacesAsToolResult(t *testing.T) {
	tr := &toolCallTransport{}
	srv := &erroringToolServer{}
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: srv},
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager(), nil)

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))

	var (
		toolResult   *message.ToolResultBlock
		resultMsg    *message.ResultMessage
		assistantSaw int
	)
	for msg := range msgs {
		switch m := msg.(type) {
		case *message.AssistantMessage:
			assistantSaw++
			for _, block := range m.Content {
				if trb, ok := block.(*message.ToolResultBlock); ok {
					toolResult = trb
				}
			}
		case *message.ResultMessage:
			resultMsg = m
		}
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}

	if gotErr != nil {
		t.Fatalf("tool error should not terminate the session, but got err: %v", gotErr)
	}
	if srv.calls != 1 {
		t.Fatalf("expected exactly one tool invocation, got %d", srv.calls)
	}
	if toolResult == nil {
		t.Fatal("expected an is_error ToolResultBlock to be emitted back to the model")
	}
	if !toolResult.IsError {
		t.Fatalf("expected ToolResultBlock.IsError=true, got %+v", toolResult)
	}
	if len(toolResult.Content) == 0 {
		t.Fatalf("expected error text in ToolResultBlock.Content, got %+v", toolResult)
	}
	if tb, ok := toolResult.Content[0].(*message.TextBlock); !ok || !strings.Contains(tb.Text, "SyntaxError") {
		t.Fatalf("expected SyntaxError text in tool result, got %+v", toolResult.Content[0])
	}
	if toolResult.ToolUseID != "call_1" {
		t.Fatalf("expected ToolUseID to match the failing tool_use id, got %q", toolResult.ToolUseID)
	}
	if tr.call < 2 {
		t.Fatalf("expected a follow-up turn after tool failure, transport called %d times", tr.call)
	}
	if resultMsg == nil {
		t.Fatal("expected a terminal ResultMessage")
	}
	if resultMsg.IsError {
		t.Fatalf("session should finish cleanly after the model self-corrects, got error result: %+v", resultMsg)
	}
	if assistantSaw == 0 {
		t.Fatalf("expected at least one assistant message to be emitted")
	}
}
