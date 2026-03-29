package openroutersdk

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestQueryStream_YieldsAcrossMultipleResults(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := &scriptedTransport{
		t: t,
		scripts: []func(*ChatRequest) ([]map[string]any, error){
			func(*ChatRequest) ([]map[string]any, error) {
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta":         map[string]any{"content": "first"},
								"finish_reason": "stop",
							},
						},
					},
				}, nil
			},
			func(*ChatRequest) ([]map[string]any, error) {
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta":         map[string]any{"content": "second"},
								"finish_reason": "stop",
							},
						},
					},
				}, nil
			},
		},
	}

	stream := MessagesFromSlice([]StreamingMessage{
		NewUserMessage(Text("hello")),
		NewUserMessage(Text("again")),
	})

	var results int
	for msg, err := range QueryStream(ctx, stream, WithTransport(tr), WithMaxTurns(1)) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := msg.(*ResultMessage); ok {
			results++
		}
	}

	if results != 2 {
		t.Fatalf("expected 2 result messages from multi-turn stream, got %d", results)
	}
}

func TestQuery_WithOnUserInputHandlesPromptTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var callbackReq *UserInputRequest
	tr := &scriptedTransport{
		t: t,
		scripts: []func(*ChatRequest) ([]map[string]any, error){
			func(req *ChatRequest) ([]map[string]any, error) {
				if len(req.Tools) == 0 {
					t.Fatalf("expected auto-registered user input tool")
				}
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta": map[string]any{
									"tool_calls": []any{
										map[string]any{
											"index": 0.0,
											"id":    "call_1",
											"function": map[string]any{
												"name":      "mcp__sdk__stdio",
												"arguments": `{"questions":[{"id":"approval","header":"Approval","question":"Continue?","multi_select":true,"options":[{"label":"yes","description":"continue"}]}]}`,
											},
										},
									},
								},
								"finish_reason": "tool_calls",
							},
						},
					},
				}, nil
			},
			func(req *ChatRequest) ([]map[string]any, error) {
				foundToolResult := false
				for _, msg := range req.Messages {
					if msg["role"] != "tool" {
						continue
					}
					content, _ := msg["content"].(string)
					if strings.Contains(content, `"approval":["yes"]`) {
						foundToolResult = true
					}
				}
				if !foundToolResult {
					t.Fatalf("expected serialized user input tool result in request history: %#v", req.Messages)
				}
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta":         map[string]any{"content": "continued"},
								"finish_reason": "stop",
							},
						},
					},
				}, nil
			},
		},
	}

	var gotResult *ResultMessage
	for msg, err := range Query(ctx, Text("Ask for confirmation before continuing."),
		WithTransport(tr),
		WithOnUserInput(func(_ context.Context, req *UserInputRequest) (*UserInputResponse, error) {
			callbackReq = req
			return &UserInputResponse{
				Answers: map[string]*UserInputAnswer{
					"approval": {Answers: []string{"yes"}},
				},
			}, nil
		}),
		WithMaxTurns(3),
	) {
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}
		if result, ok := msg.(*ResultMessage); ok {
			gotResult = result
		}
	}

	if callbackReq == nil || len(callbackReq.Questions) != 1 {
		t.Fatalf("expected callback request, got %#v", callbackReq)
	}
	if callbackReq.Questions[0].ID != "approval" {
		t.Fatalf("unexpected callback question: %#v", callbackReq.Questions[0])
	}
	if !callbackReq.Questions[0].MultiSelect {
		t.Fatalf("expected multi-select question, got %#v", callbackReq.Questions[0])
	}
	if gotResult == nil || gotResult.Result == nil || *gotResult.Result != "continued" {
		t.Fatalf("unexpected result: %#v", gotResult)
	}
}

func TestQuery_OnUserInputConflictYieldsError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	for _, err := range Query(ctx, Text("hi"),
		WithOnUserInput(func(context.Context, *UserInputRequest) (*UserInputResponse, error) {
			return nil, nil
		}),
		WithPermissionPromptToolName("custom"),
	) {
		if err == nil {
			continue
		}
		if !strings.Contains(err.Error(), "on_user_input callback cannot be used with permission_prompt_tool_name") {
			t.Fatalf("unexpected error: %v", err)
		}
		return
	}

	t.Fatal("expected iterator error")
}

func TestQuery_WithSDKToolsExecutesEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tr := &scriptedTransport{
		t: t,
		scripts: []func(*ChatRequest) ([]map[string]any, error){
			func(req *ChatRequest) ([]map[string]any, error) {
				foundTool := false
				for _, tool := range req.Tools {
					function, _ := tool["function"].(map[string]any)
					if function["name"] == "mcp__sdk__echo" {
						foundTool = true
					}
				}
				if !foundTool {
					t.Fatalf("expected registered sdk tool in request: %#v", req.Tools)
				}
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta": map[string]any{
									"tool_calls": []any{
										map[string]any{
											"index": 0.0,
											"id":    "call_1",
											"function": map[string]any{
												"name":      "mcp__sdk__echo",
												"arguments": `{"text":"hello"}`,
											},
										},
									},
								},
								"finish_reason": "tool_calls",
							},
						},
					},
				}, nil
			},
			func(req *ChatRequest) ([]map[string]any, error) {
				foundToolResult := false
				for _, msg := range req.Messages {
					if msg["role"] != "tool" {
						continue
					}
					content, _ := msg["content"].(string)
					if strings.Contains(content, `"echo":"hello"`) {
						foundToolResult = true
					}
				}
				if !foundToolResult {
					t.Fatalf("expected sdk tool result in request history: %#v", req.Messages)
				}
				return []map[string]any{
					{
						"choices": []any{
							map[string]any{
								"delta":         map[string]any{"content": "tool complete"},
								"finish_reason": "stop",
							},
						},
					},
				}, nil
			},
		},
	}

	tool := NewTool("echo", "Echo text", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"text": map[string]any{"type": "string"},
		},
		"required": []string{"text"},
	}, func(_ context.Context, input map[string]any) (map[string]any, error) {
		return map[string]any{"echo": input["text"]}, nil
	})

	var gotResult *ResultMessage
	for msg, err := range Query(ctx, Text("Use the echo tool."), WithTransport(tr), WithSDKTools(tool), WithMaxTurns(3)) {
		if err != nil {
			t.Fatalf("unexpected query error: %v", err)
		}
		if result, ok := msg.(*ResultMessage); ok {
			gotResult = result
		}
	}

	if gotResult == nil || gotResult.Result == nil || *gotResult.Result != "tool complete" {
		t.Fatalf("unexpected result: %#v", gotResult)
	}
}
