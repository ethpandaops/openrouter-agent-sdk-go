package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	sdkerrors "github.com/ethpandaops/openrouter-agent-sdk-go/internal/errors"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/hook"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/mcp"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/permission"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
)

type captureTransport struct {
	requests []*config.ChatRequest
	content  string
}

func (t *captureTransport) Start(context.Context) error { return nil }
func (t *captureTransport) Close() error                { return nil }
func (t *captureTransport) CreateStream(
	_ context.Context,
	req *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.requests = append(t.requests, req)
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	content := "done"
	if t.content != "" {
		content = t.content
	}
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": content},
				"finish_reason": "stop",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessagesInjectsSystemPrompt(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport:    tr,
		SystemPrompt: "system text",
		MaxTurns:     1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})
	var sawSystem *message.SystemMessage
	for msg := range msgs {
		if sm, ok := msg.(*message.SystemMessage); ok {
			sawSystem = sm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(tr.requests) == 0 {
		t.Fatalf("expected at least one request")
	}
	req := tr.requests[0]
	if len(req.Messages) < 2 {
		t.Fatalf("expected system+user messages, got %#v", req.Messages)
	}
	if req.Messages[0]["role"] != "system" || req.Messages[0]["content"] != "system text" {
		t.Fatalf("unexpected first message: %#v", req.Messages[0])
	}
	if sawSystem == nil || sawSystem.Subtype != "init" {
		t.Fatalf("expected emitted init system message, got %#v", sawSystem)
	}
	if got, _ := sawSystem.Data["prompt"].(string); got != "system text" {
		t.Fatalf("unexpected system message prompt: %#v", sawSystem.Data)
	}
}

func TestRunMessagesChatCarriesMultimodalInputBlocks(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role: "user",
				Content: message.NewUserMessageContentBlocks([]message.ContentBlock{
					&message.TextBlock{Type: message.BlockTypeText, Text: "Compare the assets."},
					&message.InputImageBlock{
						Type:     message.BlockTypeImageURL,
						ImageURL: message.InputImageRef{URL: "data:image/png;base64,aGVsbG8="},
					},
					&message.InputFileBlock{
						Type: message.BlockTypeFile,
						File: message.InputFileRef{
							Filename: "spec.pdf",
							FileData: "data:application/pdf;base64,JVBERi0xLjQK",
						},
					},
					&message.InputAudioBlock{
						Type: message.BlockTypeInputAudio,
						InputAudio: message.InputAudioRef{
							Format: "wav",
							Data:   "UklGRg==",
						},
					},
					&message.InputVideoBlock{
						Type:     message.BlockTypeVideoURL,
						VideoURL: message.InputVideoRef{URL: "https://example.com/demo.mp4"},
					},
				}),
			},
		},
	})
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(tr.requests) != 1 {
		t.Fatalf("expected one request, got %d", len(tr.requests))
	}
	content, ok := tr.requests[0].Messages[0]["content"].([]message.ContentBlock)
	if !ok {
		t.Fatalf("expected block content slice, got %#v", tr.requests[0].Messages[0]["content"])
	}
	if len(content) != 5 {
		t.Fatalf("expected 5 content blocks, got %#v", content)
	}
	if _, ok := content[1].(*message.InputImageBlock); !ok {
		t.Fatalf("expected image input block, got %#v", content[1])
	}
	if _, ok := content[2].(*message.InputFileBlock); !ok {
		t.Fatalf("expected file input block, got %#v", content[2])
	}
	if _, ok := content[3].(*message.InputAudioBlock); !ok {
		t.Fatalf("expected audio input block, got %#v", content[3])
	}
	if _, ok := content[4].(*message.InputVideoBlock); !ok {
		t.Fatalf("expected video input block, got %#v", content[4])
	}
}

func TestRunMessagesResponsesRejectsMultimodalInput(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport:         tr,
		OpenRouterAPIMode: config.OpenRouterAPIModeResponses,
		MaxTurns:          1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role: "user",
				Content: message.NewUserMessageContentBlocks([]message.ContentBlock{
					&message.TextBlock{Type: message.BlockTypeText, Text: "Describe this image."},
					&message.InputImageBlock{
						Type:     message.BlockTypeImageURL,
						ImageURL: message.InputImageRef{URL: "data:image/png;base64,aGVsbG8="},
					},
				}),
			},
		},
	})
	for range msgs {
	}

	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	if gotErr == nil || !strings.Contains(gotErr.Error(), "multimodal input requires chat/completions api mode") {
		t.Fatalf("expected multimodal responses error, got %v", gotErr)
	}
	if len(tr.requests) != 0 {
		t.Fatalf("expected no transport requests, got %d", len(tr.requests))
	}
}

func TestRunMessagesSystemPromptPresetAppendPrecedence(t *testing.T) {
	tr := &captureTransport{}
	appendText := "append text"
	opts := &config.Options{
		Transport:    tr,
		SystemPrompt: "system text",
		SystemPromptPreset: &config.SystemPromptPreset{
			Type:   "preset",
			Preset: "claude_code",
			Append: &appendText,
		},
		MaxTurns: 1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	req := tr.requests[0]
	if req.Messages[0]["content"] != "append text" {
		t.Fatalf("expected preset append to win, got %#v", req.Messages[0])
	}
}

func TestRunMessagesParsesStructuredOutput(t *testing.T) {
	tr := &captureTransport{content: `{"answer":"ok"}`}
	opts := &config.Options{
		Transport: tr,
		OutputFormat: map[string]any{
			"type": "json_schema",
			"schema": map[string]any{
				"type": "object",
			},
		},
		MaxTurns: 1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var result *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			result = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result == nil {
		t.Fatalf("expected result message")
	}
	if result.DurationMs <= 0 {
		t.Fatalf("expected duration to be populated, got %+v", result)
	}
	if result.DurationAPIMs <= 0 {
		t.Fatalf("expected duration_api_ms to be populated, got %+v", result)
	}
	m, ok := result.StructuredOutput.(map[string]any)
	if !ok || m["answer"] != "ok" {
		t.Fatalf("expected structured output, got %#v", result.StructuredOutput)
	}
}

type responsesDuplicateTransport struct{}

func (t *responsesDuplicateTransport) Start(context.Context) error { return nil }
func (t *responsesDuplicateTransport) Close() error                { return nil }
func (t *responsesDuplicateTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 8)
	errs := make(chan error, 1)
	out <- map[string]any{"type": "response.created"}
	out <- map[string]any{"type": "response.output_text.delta", "delta": "Hello"}
	out <- map[string]any{"type": "response.output_text.delta", "delta": " world"}
	out <- map[string]any{"type": "response.output_text.done", "text": "Hello world"}
	out <- map[string]any{
		"type": "response.output_item.done",
		"item": map[string]any{
			"type": "message",
			"content": []any{
				map[string]any{"type": "output_text", "text": "Hello world"},
			},
		},
	}
	out <- map[string]any{"type": "response.completed"}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessagesResponsesDeduplicatesTerminalTextEvents(t *testing.T) {
	tr := &responsesDuplicateTransport{}
	opts := &config.Options{
		Transport:         tr,
		OpenRouterAPIMode: config.OpenRouterAPIModeResponses,
		MaxTurns:          1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var (
		result         *message.ResultMessage
		assistantCount int
	)
	for msg := range msgs {
		switch m := msg.(type) {
		case *message.AssistantMessage:
			assistantCount++
			if len(m.Content) != 1 {
				t.Fatalf("unexpected assistant content: %#v", m.Content)
			}
			tb, ok := m.Content[0].(*message.TextBlock)
			if !ok || tb.Text != "Hello world" {
				t.Fatalf("unexpected assistant text: %#v", m.Content)
			}
		case *message.ResultMessage:
			result = m
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if assistantCount != 1 {
		t.Fatalf("expected exactly one final assistant message, got %d", assistantCount)
	}
	if result == nil || result.Result == nil || *result.Result != "Hello world" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

type finishReasonOnlyTransport struct{}

func (t *finishReasonOnlyTransport) Start(context.Context) error { return nil }
func (t *finishReasonOnlyTransport) Close() error                { return nil }
func (t *finishReasonOnlyTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 2)
	errs := make(chan error, 1)
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{},
				"finish_reason": "length",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessagesDoesNotLeakFinishReasonIntoResultText(t *testing.T) {
	tr := &finishReasonOnlyTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var result *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			result = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result == nil {
		t.Fatalf("expected result message")
	}
	if result.Result != nil {
		t.Fatalf("expected nil result text when stream only reports finish reason, got %q", *result.Result)
	}
}

type imageTransport struct{}

func (t *imageTransport) Start(context.Context) error { return nil }
func (t *imageTransport) Close() error                { return nil }
func (t *imageTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 2)
	errs := make(chan error, 1)
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta": map[string]any{
					"content": "icon",
					"images": []any{
						map[string]any{
							"type": "image_url",
							"image_url": map[string]any{
								"url": "data:image/png;base64,aGVsbG8=",
							},
							"media_type": "image/png",
						},
					},
				},
				"finish_reason": "stop",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessagesEmitsAssistantImageBlocks(t *testing.T) {
	tr := &imageTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("draw"),
			},
		},
	})

	var assistant *message.AssistantMessage
	for msg := range msgs {
		if am, ok := msg.(*message.AssistantMessage); ok {
			assistant = am
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if assistant == nil {
		t.Fatal("expected assistant message")
	}
	if len(assistant.Content) != 2 {
		t.Fatalf("expected text+image blocks, got %#v", assistant.Content)
	}
	if _, ok := assistant.Content[0].(*message.TextBlock); !ok {
		t.Fatalf("expected first block text, got %#v", assistant.Content[0])
	}
	image, ok := assistant.Content[1].(*message.ImageBlock)
	if !ok {
		t.Fatalf("expected second block image, got %#v", assistant.Content[1])
	}
	if image.MediaType != "image/png" || image.URL != "data:image/png;base64,aGVsbG8=" {
		t.Fatalf("unexpected image block: %#v", image)
	}
}

func TestRunMessagesPartialAssistantSkipsFinalDuplicateAssistant(t *testing.T) {
	tr := &responsesDuplicateTransport{}
	opts := &config.Options{
		Transport:              tr,
		OpenRouterAPIMode:      config.OpenRouterAPIModeResponses,
		IncludePartialMessages: true,
		MaxTurns:               1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var assistantTexts []string
	for msg := range msgs {
		if am, ok := msg.(*message.AssistantMessage); ok {
			if len(am.Content) != 1 {
				t.Fatalf("unexpected assistant content: %#v", am.Content)
			}
			tb, ok := am.Content[0].(*message.TextBlock)
			if !ok {
				t.Fatalf("unexpected assistant block: %#v", am.Content[0])
			}
			assistantTexts = append(assistantTexts, tb.Text)
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(assistantTexts) != 2 {
		t.Fatalf("expected only partial assistant messages, got %#v", assistantTexts)
	}
	if assistantTexts[0] != "Hello" || assistantTexts[1] != " world" {
		t.Fatalf("unexpected partial assistant texts: %#v", assistantTexts)
	}
}

type toolCallTransport struct {
	call int
}

func (t *toolCallTransport) Start(context.Context) error { return nil }
func (t *toolCallTransport) Close() error                { return nil }
func (t *toolCallTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.call++
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	if t.call%2 == 1 {
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0.0,
								"id":    "call_1",
								"function": map[string]any{
									"name":      "mcp__srv__echo",
									"arguments": "{}",
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
	} else {
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta":         map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		}
	}
	close(out)
	close(errs)
	return out, errs
}

type toolCallCostTransport struct {
	call int
}

func (t *toolCallCostTransport) Start(context.Context) error { return nil }
func (t *toolCallCostTransport) Close() error                { return nil }
func (t *toolCallCostTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.call++
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	if t.call == 1 {
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0.0,
								"id":    "call_1",
								"function": map[string]any{
									"name":      "mcp__srv__echo",
									"arguments": `{"text":"hello"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"total_cost_usd": 0.01,
		}
	} else {
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta":         map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
			"total_cost_usd": 0.02,
		}
	}
	close(out)
	close(errs)
	return out, errs
}

type fallbackTransport struct {
	models []string
}

func (t *fallbackTransport) Start(context.Context) error { return nil }
func (t *fallbackTransport) Close() error                { return nil }
func (t *fallbackTransport) CreateStream(
	_ context.Context,
	req *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.models = append(t.models, req.Model)
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	if req.Model == "primary-model" {
		close(out)
		errs <- context.DeadlineExceeded
		close(errs)
		return out, errs
	}
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "fallback-ok"},
				"finish_reason": "stop",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

type fallbackAfterEmitTransport struct {
	models []string
}

func (t *fallbackAfterEmitTransport) Start(context.Context) error { return nil }
func (t *fallbackAfterEmitTransport) Close() error                { return nil }
func (t *fallbackAfterEmitTransport) CreateStream(
	_ context.Context,
	req *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.models = append(t.models, req.Model)
	out := make(chan map[string]any, 2)
	errs := make(chan error, 1)
	if req.Model == "primary-model" {
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{"content": "partial"},
				},
			},
		}
		close(out)
		errs <- context.Canceled
		close(errs)
		return out, errs
	}
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "fallback-ok"},
				"finish_reason": "stop",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

type usageMetadataTransport struct{}

func (t *usageMetadataTransport) Start(context.Context) error { return nil }
func (t *usageMetadataTransport) Close() error                { return nil }
func (t *usageMetadataTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "done"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     7.0,
			"completion_tokens": 3.0,
		},
		"total_cost_usd": 0.01,
	}
	close(out)
	close(errs)
	return out, errs
}

type testToolServer struct{}

func (s *testToolServer) Name() string    { return "srv" }
func (s *testToolServer) Version() string { return "1.0.0" }
func (s *testToolServer) ListTools() []map[string]any {
	return []map[string]any{
		{
			"name":        "echo",
			"description": "echo",
			"inputSchema": map[string]any{"type": "object"},
		},
	}
}
func (s *testToolServer) CallTool(_ context.Context, _ string, input map[string]any) (map[string]any, error) {
	text := "ok"
	if v, ok := input["text"]; ok {
		if str, ok := v.(string); ok && str != "" {
			text = str
		}
	}
	return map[string]any{
		"content": []any{
			map[string]any{"type": "text", "text": text},
		},
	}, nil
}

func TestRunMessagesEmitsPermissionRequestHook(t *testing.T) {
	tr := &toolCallTransport{}
	count := 0
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		Hooks: map[hook.Event][]*hook.Matcher{
			hook.EventPermissionRequest: {
				{
					Hooks: []hook.Callback{
						func(
							_ context.Context,
							_ hook.Input,
							_ *string,
							_ *hook.Context,
						) (hook.JSONOutput, error) {
							count++
							return &hook.SyncJSONOutput{}, nil
						},
					},
				},
			},
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if count == 0 {
		t.Fatalf("expected permission request hook to run")
	}
}

func TestRunMessagesPersistsAssistantToolCallsBeforeToolResults(t *testing.T) {
	tr := &toolCallTransport{}
	sessions := session.NewManager()
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, sessions)

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	s, ok := sessions.Get("default")
	if !ok {
		t.Fatal("expected persisted session")
	}
	if len(s.Messages) < 4 {
		t.Fatalf("expected persisted history, got %#v", s.Messages)
	}
	if s.Messages[1]["role"] != "assistant" {
		t.Fatalf("expected assistant tool-call message before tool result, got %#v", s.Messages)
	}
	if _, ok := s.Messages[1]["tool_calls"]; !ok {
		t.Fatalf("expected assistant tool_calls history, got %#v", s.Messages[1])
	}
	if s.Messages[2]["role"] != "tool" {
		t.Fatalf("expected tool result after assistant tool_calls, got %#v", s.Messages)
	}
}

func TestRunMessagesAccumulatesTotalCostAcrossToolLoops(t *testing.T) {
	tr := &toolCallCostTransport{}
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if gotResult == nil {
		t.Fatal("expected result message")
	}
	if gotResult.TotalCostUSD == nil || *gotResult.TotalCostUSD != 0.03 {
		t.Fatalf("expected accumulated total cost, got %+v", gotResult.TotalCostUSD)
	}
}

func TestRunMessagesInterruptDeniedToolEmitsErrorResult(t *testing.T) {
	tr := &toolCallTransport{}
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		CanUseTool: func(_ context.Context, _ string, _ map[string]any, _ *permission.Context) (permission.Result, error) {
			return &permission.ResultDeny{
				Behavior:  "deny",
				Message:   "nope",
				Interrupt: true,
			}, nil
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))

	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error channel value: %v", err)
		}
	}
	if gotResult == nil {
		t.Fatalf("expected result message")
	}
	if !gotResult.IsError {
		t.Fatalf("expected error result, got %+v", gotResult)
	}
}

func TestPermissionRequestHookDecisionDenyInterrupt(t *testing.T) {
	tr := &toolCallTransport{}
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		Hooks: map[hook.Event][]*hook.Matcher{
			hook.EventPermissionRequest: {
				{
					Hooks: []hook.Callback{
						func(context.Context, hook.Input, *string, *hook.Context) (hook.JSONOutput, error) {
							return &hook.SyncJSONOutput{
								HookSpecificOutput: &hook.PermissionRequestSpecificOutput{
									HookEventName: "PermissionRequest",
									Decision: map[string]any{
										"behavior":  "deny",
										"message":   "blocked-by-hook",
										"interrupt": true,
									},
								},
							}, nil
						},
					},
				},
			},
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if gotResult == nil || !gotResult.IsError {
		t.Fatalf("expected error result from hook deny interrupt, got %+v", gotResult)
	}
	if gotResult.Result == nil || *gotResult.Result != "blocked-by-hook" {
		t.Fatalf("unexpected result text: %+v", gotResult.Result)
	}
}

func TestPermissionRequestSuggestionsFlowIntoCanUseTool(t *testing.T) {
	tr := &toolCallTransport{}
	seenSuggestions := 0
	seenInputText := ""
	allow := permission.BehaviorAllow
	opts := &config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		Hooks: map[hook.Event][]*hook.Matcher{
			hook.EventPermissionRequest: {
				{
					Hooks: []hook.Callback{
						func(context.Context, hook.Input, *string, *hook.Context) (hook.JSONOutput, error) {
							return &hook.SyncJSONOutput{
								HookSpecificOutput: &hook.PermissionRequestSpecificOutput{
									HookEventName: "PermissionRequest",
									Decision: map[string]any{
										"behavior": "allow",
										"updatedInput": map[string]any{
											"text": "from-hook",
										},
										"suggestions": []any{
											map[string]any{
												"type":     string(permission.UpdateTypeAddRules),
												"behavior": string(allow),
												"rules": []any{
													map[string]any{"toolName": "mcp__srv__echo"},
												},
											},
										},
									},
								},
							}, nil
						},
					},
				},
			},
		},
		CanUseTool: func(_ context.Context, _ string, input map[string]any, permCtx *permission.Context) (permission.Result, error) {
			seenSuggestions = len(permCtx.Suggestions)
			if t, ok := input["text"].(string); ok {
				seenInputText = t
			}
			return &permission.ResultAllow{Behavior: "allow"}, nil
		},
		MaxTurns: 2,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if seenSuggestions != 1 {
		t.Fatalf("expected 1 suggestion in callback, got %d", seenSuggestions)
	}
	if seenInputText != "from-hook" {
		t.Fatalf("expected updated input text from hook, got %q", seenInputText)
	}
}

func TestRunMessagesUsesFallbackModelOnPrimaryFailure(t *testing.T) {
	tr := &fallbackTransport{}
	opts := &config.Options{
		Transport:     tr,
		Model:         "primary-model",
		FallbackModel: "fallback-model",
		MaxTurns:      1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if gotResult == nil || gotResult.Result == nil || *gotResult.Result != "fallback-ok" {
		t.Fatalf("expected fallback result, got %+v", gotResult)
	}
	if len(tr.models) < 2 {
		t.Fatalf("expected primary then fallback model attempts, got %#v", tr.models)
	}
	if tr.models[0] != "primary-model" || tr.models[1] != "fallback-model" {
		t.Fatalf("unexpected model attempt order: %#v", tr.models)
	}
}

func TestRunMessagesNoFallbackAfterPrimaryEmitsOutput(t *testing.T) {
	tr := &fallbackAfterEmitTransport{}
	opts := &config.Options{
		Transport:     tr,
		Model:         "primary-model",
		FallbackModel: "fallback-model",
		MaxTurns:      1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	gotErr := false
	for err := range errs {
		if err != nil {
			gotErr = true
		}
	}
	if !gotErr {
		t.Fatalf("expected stream error from primary model")
	}
	if len(tr.models) != 1 || tr.models[0] != "primary-model" {
		t.Fatalf("expected no fallback attempt after emitted output, got %#v", tr.models)
	}
}

func TestRunMessagesPopulatesUsageAndCostMetadata(t *testing.T) {
	tr := &usageMetadataTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if gotResult == nil {
		t.Fatalf("expected result message")
	}
	if gotResult.Usage == nil || gotResult.Usage.InputTokens != 7 || gotResult.Usage.OutputTokens != 3 {
		t.Fatalf("expected usage metadata, got %+v", gotResult.Usage)
	}
	if gotResult.TotalCostUSD == nil || *gotResult.TotalCostUSD != 0.01 {
		t.Fatalf("expected total_cost_usd metadata, got %+v", gotResult.TotalCostUSD)
	}
}

func TestRunMessagesRespectsMaxBudgetUSD(t *testing.T) {
	tr := &usageMetadataTransport{}
	maxBudget := 0.001
	opts := &config.Options{
		Transport:    tr,
		MaxTurns:     1,
		MaxBudgetUSD: &maxBudget,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	var gotResult *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			gotResult = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if gotResult == nil {
		t.Fatalf("expected result message")
	}
	if !gotResult.IsError {
		t.Fatalf("expected budget error result, got %+v", gotResult)
	}
	if gotResult.Subtype != "error_max_budget_usd" {
		t.Fatalf("expected budget subtype error_max_budget_usd, got %q", gotResult.Subtype)
	}
	if gotResult.Result == nil || *gotResult.Result == "" {
		t.Fatalf("expected error message")
	}
}

func TestRunMessagesAddsUserMetadataToRequest(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport: tr,
		User:      "user-123",
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(tr.requests) == 0 {
		t.Fatalf("expected request")
	}
	meta := tr.requests[0].Metadata
	if meta == nil || meta["user"] != "user-123" {
		t.Fatalf("expected user metadata, got %#v", meta)
	}
	if tr.requests[0].User != "user-123" {
		t.Fatalf("expected user request field, got %#v", tr.requests[0].User)
	}
}

func TestRunMessagesMapsExpandedOpenRouterFieldsToRequest(t *testing.T) {
	tr := &captureTransport{}
	topP := 0.9
	temperature := 0.2
	maxOut := 256
	seed := int64(123)
	topLogprobs := 2
	parallel := true
	trace := true
	background := true
	store := false
	maxToolCalls := 3
	opts := &config.Options{
		Transport:                    tr,
		MaxTurns:                     1,
		OpenRouterTopP:               &topP,
		OpenRouterTemperature:        &temperature,
		OpenRouterMaxOutputTokens:    &maxOut,
		OpenRouterSeed:               &seed,
		OpenRouterTopLogprobs:        &topLogprobs,
		OpenRouterParallelToolCalls:  &parallel,
		OpenRouterProvider:           map[string]any{"order": []any{"openai"}},
		OpenRouterPlugins:            []map[string]any{{"id": "auto-router"}},
		OpenRouterRoute:              "fallback",
		OpenRouterReasoning:          map[string]any{"effort": "high"},
		OpenRouterSessionID:          "session-x",
		OpenRouterTrace:              &trace,
		OpenRouterModalities:         []string{"text"},
		OpenRouterImageConfig:        map[string]any{"size": "1024x1024"},
		OpenRouterInstructions:       "be concise",
		OpenRouterPreviousResponseID: "resp_1",
		OpenRouterPromptCacheKey:     "cache_1",
		OpenRouterMaxToolCalls:       &maxToolCalls,
		OpenRouterServiceTier:        "auto",
		OpenRouterTruncation:         "auto",
		OpenRouterInclude:            []string{"reasoning.encrypted_content"},
		OpenRouterBackground:         &background,
		OpenRouterSafetyIdentifier:   "safe-1",
		OpenRouterStore:              &store,
		OpenRouterPrompt:             map[string]any{"id": "prompt_1"},
		OpenRouterText:               map[string]any{"verbosity": "low"},
		OpenRouterMetadata:           map[string]any{"k": "v"},
		OpenRouterExtra:              map[string]any{"trace": true},
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(tr.requests) == 0 {
		t.Fatalf("expected request")
	}
	got := tr.requests[0]
	if got.TopP == nil || *got.TopP != topP {
		t.Fatalf("expected top_p, got %#v", got.TopP)
	}
	if got.Temperature == nil || *got.Temperature != temperature {
		t.Fatalf("expected temperature, got %#v", got.Temperature)
	}
	if got.MaxOutputTokens == nil || *got.MaxOutputTokens != maxOut {
		t.Fatalf("expected max_output_tokens, got %#v", got.MaxOutputTokens)
	}
	if got.Seed == nil || *got.Seed != seed {
		t.Fatalf("expected seed, got %#v", got.Seed)
	}
	if got.Provider == nil || got.Provider["order"] == nil {
		t.Fatalf("expected provider map, got %#v", got.Provider)
	}
	if len(got.Plugins) != 1 {
		t.Fatalf("expected plugins, got %#v", got.Plugins)
	}
	if got.Route != "fallback" {
		t.Fatalf("expected route fallback, got %#v", got.Route)
	}
	if got.SessionID != "session-x" {
		t.Fatalf("expected session id, got %#v", got.SessionID)
	}
	if got.Metadata == nil || got.Metadata["k"] != "v" {
		t.Fatalf("expected metadata, got %#v", got.Metadata)
	}
	if got.Background == nil || !*got.Background {
		t.Fatalf("expected background true, got %#v", got.Background)
	}
	if got.Store == nil || *got.Store {
		t.Fatalf("expected store false, got %#v", got.Store)
	}
	if got.Extra == nil || got.Extra["trace"] != true {
		t.Fatalf("expected extra fields, got %#v", got.Extra)
	}
}

func TestRunMessagesUnsupportedSyncHookOutputReturnsTypedError(t *testing.T) {
	tr := &captureTransport{}
	suppress := true
	opts := &config.Options{
		Transport: tr,
		Hooks: map[hook.Event][]*hook.Matcher{
			hook.EventUserPromptSubmit: {
				{
					Hooks: []hook.Callback{
						func(context.Context, hook.Input, *string, *hook.Context) (hook.JSONOutput, error) {
							return &hook.SyncJSONOutput{
								SuppressOutput: &suppress,
							}, nil
						},
					},
				},
			},
		},
		MaxTurns: 1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	var unsupported *sdkerrors.UnsupportedHookOutputError
	if gotErr == nil || !errors.As(gotErr, &unsupported) {
		t.Fatalf("expected UnsupportedHookOutputError, got %v", gotErr)
	}
	if unsupported.Event != string(hook.EventUserPromptSubmit) {
		t.Fatalf("unexpected event: %q", unsupported.Event)
	}
	if unsupported.Field != "suppressOutput" {
		t.Fatalf("unexpected field: %q", unsupported.Field)
	}
}

func TestRunMessagesUnsupportedAsyncHookOutputReturnsTypedError(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport: tr,
		Hooks: map[hook.Event][]*hook.Matcher{
			hook.EventUserPromptSubmit: {
				{
					Hooks: []hook.Callback{
						func(context.Context, hook.Input, *string, *hook.Context) (hook.JSONOutput, error) {
							return &hook.AsyncJSONOutput{Async: true}, nil
						},
					},
				},
			},
		},
		MaxTurns: 1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	var gotErr error
	for err := range errs {
		if err != nil {
			gotErr = err
		}
	}
	var unsupported *sdkerrors.UnsupportedHookOutputError
	if gotErr == nil || !errors.As(gotErr, &unsupported) {
		t.Fatalf("expected UnsupportedHookOutputError, got %v", gotErr)
	}
	if unsupported.Field != "async" {
		t.Fatalf("unexpected field: %q", unsupported.Field)
	}
}

type blockingIteratorTransport struct {
	call         int
	firstStarted chan struct{}
	unblockFirst chan struct{}
}

func (t *blockingIteratorTransport) Start(context.Context) error { return nil }
func (t *blockingIteratorTransport) Close() error                { return nil }
func (t *blockingIteratorTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.call++
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	if t.call == 1 {
		select {
		case <-t.firstStarted:
		default:
			close(t.firstStarted)
		}
		<-t.unblockFirst
	}
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "ok"},
				"finish_reason": "stop",
			},
		},
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessageIteratorConsumesInputsWhileFirstTurnRunning(t *testing.T) {
	tr := &blockingIteratorTransport{
		firstStarted: make(chan struct{}),
		unblockFirst: make(chan struct{}),
	}
	r := NewQueryRunner(&config.Options{
		Transport: tr,
		MaxTurns:  1,
	}, session.NewManager())

	secondConsumed := make(chan struct{})
	inputs := func(yield func(message.StreamingMessage) bool) {
		msg := message.StreamingMessage{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("first"),
			},
		}
		if !yield(msg) {
			return
		}
		msg.Message.Content = message.NewUserMessageContent("second")
		if !yield(msg) {
			return
		}
		close(secondConsumed)
	}

	msgs, errs := r.RunMessageIterator(context.Background(), "default", inputs)

	select {
	case <-tr.firstStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("first turn did not start")
	}

	select {
	case <-secondConsumed:
		// Input iterator was consumed while first turn was still blocked.
	case <-time.After(200 * time.Millisecond):
		t.Fatalf("second input was not consumed while first turn was running")
	}

	close(tr.unblockFirst)

	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

type cancelAwareTransport struct{}

func (t *cancelAwareTransport) Start(context.Context) error { return nil }
func (t *cancelAwareTransport) Close() error                { return nil }
func (t *cancelAwareTransport) CreateStream(
	ctx context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errs)
		<-ctx.Done()
		errs <- ctx.Err()
	}()
	return out, errs
}

func TestRunMessageIteratorCancellationIsDeterministic(t *testing.T) {
	r := NewQueryRunner(&config.Options{
		Transport: &cancelAwareTransport{},
		MaxTurns:  1,
	}, session.NewManager())

	inputs := func(yield func(message.StreamingMessage) bool) {
		msg := message.StreamingMessage{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("first"),
			},
		}
		_ = yield(msg)
	}

	ctx, cancel := context.WithCancel(context.Background())
	msgs, errs := r.RunMessageIterator(ctx, "default", inputs)
	cancel()

	for range msgs {
	}
	gotCanceled := false
	for err := range errs {
		if errors.Is(err, context.Canceled) {
			gotCanceled = true
		}
	}
	if !gotCanceled {
		t.Fatalf("expected context canceled error")
	}
}

type blockingToolTurnTransport struct {
	call         int
	firstStarted chan struct{}
	unblockFirst chan struct{}
}

func (t *blockingToolTurnTransport) Start(context.Context) error { return nil }
func (t *blockingToolTurnTransport) Close() error                { return nil }
func (t *blockingToolTurnTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.call++
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	switch t.call % 2 {
	case 1:
		if t.call == 1 {
			select {
			case <-t.firstStarted:
			default:
				close(t.firstStarted)
			}
			<-t.unblockFirst
		}
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta": map[string]any{
						"tool_calls": []any{
							map[string]any{
								"index": 0.0,
								"id":    "call_tool",
								"function": map[string]any{
									"name":      "mcp__srv__echo",
									"arguments": `{"text":"ok"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		}
	default:
		out <- map[string]any{
			"choices": []any{
				map[string]any{
					"delta":         map[string]any{"content": "done"},
					"finish_reason": "stop",
				},
			},
		}
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessageIteratorToolFlowRemainsFunctionalWhileInputsArrive(t *testing.T) {
	tr := &blockingToolTurnTransport{
		firstStarted: make(chan struct{}),
		unblockFirst: make(chan struct{}),
	}
	r := NewQueryRunner(&config.Options{
		Transport: tr,
		MCPServers: map[string]mcp.ServerConfig{
			"srv": &mcp.SdkServerConfig{Type: mcp.ServerTypeSDK, Name: "srv", Instance: &testToolServer{}},
		},
		MaxTurns: 2,
	}, session.NewManager())

	secondConsumed := make(chan struct{})
	inputs := func(yield func(message.StreamingMessage) bool) {
		msg := message.StreamingMessage{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("first"),
			},
		}
		if !yield(msg) {
			return
		}
		msg.Message.Content = message.NewUserMessageContent("second")
		if !yield(msg) {
			return
		}
		close(secondConsumed)
	}

	msgs, errs := r.RunMessageIterator(context.Background(), "default", inputs)
	select {
	case <-tr.firstStarted:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("expected first tool turn to start")
	}

	select {
	case <-secondConsumed:
	case <-time.After(300 * time.Millisecond):
		t.Fatalf("expected second input consumed while first tool turn running")
	}

	close(tr.unblockFirst)

	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}

func TestResultMessageStopReasonSuccess(t *testing.T) {
	tr := &captureTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var result *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			result = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result == nil {
		t.Fatalf("expected result message")
	}
	if result.StopReason == nil || *result.StopReason != "end_turn" {
		t.Fatalf("expected stop_reason 'end_turn', got %v", result.StopReason)
	}
}

type budgetExceedTransport struct{}

func (t *budgetExceedTransport) Start(context.Context) error { return nil }
func (t *budgetExceedTransport) Close() error                { return nil }
func (t *budgetExceedTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 1)
	errs := make(chan error, 1)
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "done"},
				"finish_reason": "stop",
			},
		},
		"total_cost_usd": 999.0,
	}
	close(out)
	close(errs)
	return out, errs
}

func TestResultMessageStopReasonMaxBudget(t *testing.T) {
	budget := 0.001
	tr := &budgetExceedTransport{}
	opts := &config.Options{
		Transport:    tr,
		MaxTurns:     2,
		MaxBudgetUSD: &budget,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var result *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			result = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result == nil {
		t.Fatalf("expected result message")
	}
	if result.StopReason == nil || *result.StopReason != "max_budget" {
		t.Fatalf("expected stop_reason 'max_budget', got %v", result.StopReason)
	}
}

func TestParseUsageAndCostExtractsCachedAndReasoningTokens(t *testing.T) {
	tests := []struct {
		name             string
		raw              map[string]any
		wantCachedIn     int
		wantReasoningOut int
	}{
		{
			name: "top-level cached_input_tokens and reasoning_output_tokens",
			raw: map[string]any{
				"usage": map[string]any{
					"input_tokens":            100,
					"output_tokens":           50,
					"cached_input_tokens":     80,
					"reasoning_output_tokens": 30,
				},
			},
			wantCachedIn:     80,
			wantReasoningOut: 30,
		},
		{
			name: "OpenAI nested prompt_tokens_details and completion_tokens_details",
			raw: map[string]any{
				"usage": map[string]any{
					"prompt_tokens":     200,
					"completion_tokens": 100,
					"prompt_tokens_details": map[string]any{
						"cached_tokens": 150,
					},
					"completion_tokens_details": map[string]any{
						"reasoning_tokens": 40,
					},
				},
			},
			wantCachedIn:     150,
			wantReasoningOut: 40,
		},
		{
			name: "top-level takes priority over nested when both present",
			raw: map[string]any{
				"usage": map[string]any{
					"input_tokens":            100,
					"output_tokens":           50,
					"cached_input_tokens":     80,
					"reasoning_output_tokens": 30,
					"prompt_tokens_details": map[string]any{
						"cached_tokens": 999,
					},
					"completion_tokens_details": map[string]any{
						"reasoning_tokens": 888,
					},
				},
			},
			wantCachedIn:     80,
			wantReasoningOut: 30,
		},
		{
			name: "no cached or reasoning tokens",
			raw: map[string]any{
				"usage": map[string]any{
					"input_tokens":  100,
					"output_tokens": 50,
				},
			},
			wantCachedIn:     0,
			wantReasoningOut: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			usage, _ := parseUsageAndCost(tt.raw, nil, nil)
			if usage == nil {
				t.Fatalf("expected non-nil usage")
			}
			if usage.CachedInputTokens != tt.wantCachedIn {
				t.Fatalf("CachedInputTokens = %d, want %d", usage.CachedInputTokens, tt.wantCachedIn)
			}
			if usage.ReasoningOutputTokens != tt.wantReasoningOut {
				t.Fatalf("ReasoningOutputTokens = %d, want %d", usage.ReasoningOutputTokens, tt.wantReasoningOut)
			}
		})
	}
}

type usageWithCacheTransport struct{}

func (t *usageWithCacheTransport) Start(context.Context) error { return nil }
func (t *usageWithCacheTransport) Close() error                { return nil }
func (t *usageWithCacheTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 2)
	errs := make(chan error, 1)
	out <- map[string]any{
		"choices": []any{
			map[string]any{
				"delta":         map[string]any{"content": "response"},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{
			"input_tokens":            100,
			"output_tokens":           50,
			"cached_input_tokens":     75,
			"reasoning_output_tokens": 20,
		},
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunMessagesUsageIncludesCachedAndReasoningTokens(t *testing.T) {
	tr := &usageWithCacheTransport{}
	opts := &config.Options{
		Transport: tr,
		MaxTurns:  1,
	}
	r := NewQueryRunner(opts, session.NewManager())

	msgs, errs := r.RunMessages(context.Background(), "default", []message.StreamingMessage{
		{
			Type: "user",
			Message: message.StreamingMessageContent{
				Role:    "user",
				Content: message.NewUserMessageContent("hello"),
			},
		},
	})

	var result *message.ResultMessage
	for msg := range msgs {
		if rm, ok := msg.(*message.ResultMessage); ok {
			result = rm
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if result == nil {
		t.Fatalf("expected result message")
	}
	if result.Usage == nil {
		t.Fatalf("expected non-nil usage")
	}
	if result.Usage.CachedInputTokens != 75 {
		t.Fatalf("expected CachedInputTokens=75, got %d", result.Usage.CachedInputTokens)
	}
	if result.Usage.ReasoningOutputTokens != 20 {
		t.Fatalf("expected ReasoningOutputTokens=20, got %d", result.Usage.ReasoningOutputTokens)
	}
}
