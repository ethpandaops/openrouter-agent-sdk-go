package openrouter

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
)

func TestEndpointPathByMode(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{
		OpenRouterAPIMode: config.OpenRouterAPIModeChatCompletions,
	})
	if got := tr.endpointPath(); got != "chat/completions" {
		t.Fatalf("unexpected chat endpoint: %s", got)
	}

	tr = NewHTTPTransport(&config.Options{
		OpenRouterAPIMode: config.OpenRouterAPIModeResponses,
	})
	if got := tr.endpointPath(); got != "responses" {
		t.Fatalf("unexpected responses endpoint: %s", got)
	}
}

func TestBuildRequestBodyResponsesUsesInput(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeResponses})
	req := &config.ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	}
	body := tr.buildRequestBody(req)
	if _, ok := body["input"]; !ok {
		t.Fatalf("expected input for responses body: %#v", body)
	}
	if _, ok := body["messages"]; ok {
		t.Fatalf("responses body should not include messages: %#v", body)
	}
}

func TestBuildRequestBodyChatNormalizesJSONSchemaFormat(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeChatCompletions})
	req := &config.ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
		ResponseFormat: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"answer": map[string]any{"type": "string"},
			},
		},
	}

	body := tr.buildRequestBody(req)
	rf, ok := body["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("expected response_format object, got %#v", body["response_format"])
	}
	if rf["type"] != "json_schema" {
		t.Fatalf("expected wrapped json_schema type, got %#v", rf)
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected json_schema object, got %#v", rf)
	}
	schema, ok := js["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("expected wrapped raw schema object, got %#v", js["schema"])
	}
	if js["name"] != "response" {
		t.Fatalf("expected default schema name, got %#v", js["name"])
	}
}

func TestBuildRequestBodyResponsesNormalizesJSONSchemaFormat(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeResponses})
	req := &config.ChatRequest{
		Model:    "openai/gpt-4o-mini",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
		ResponseFormat: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"answer": map[string]any{"type": "string"},
			},
		},
	}

	body := tr.buildRequestBody(req)
	textObj, ok := body["text"].(map[string]any)
	if !ok {
		t.Fatalf("expected text object, got %#v", body["text"])
	}
	formatObj, ok := textObj["format"].(map[string]any)
	if !ok {
		t.Fatalf("expected text.format object, got %#v", textObj["format"])
	}
	if formatObj["type"] != "json_schema" {
		t.Fatalf("expected wrapped json_schema type, got %#v", formatObj)
	}
	if formatObj["name"] != "response" {
		t.Fatalf("expected default name, got %#v", formatObj["name"])
	}
	schema, ok := formatObj["schema"].(map[string]any)
	if !ok || schema["type"] != "object" {
		t.Fatalf("expected wrapped raw schema object, got %#v", formatObj["schema"])
	}
}

func TestBuildRequestBodyChatIncludesExpandedFields(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeChatCompletions})
	maxTokens := 120
	temp := 0.2
	topP := 0.9
	presence := 0.1
	frequency := 0.2
	seed := int64(42)
	logprobs := true
	topLogprobs := 3
	parallel := true
	trace := true
	req := &config.ChatRequest{
		Model:             "openai/gpt-4o-mini",
		Messages:          []map[string]any{{"role": "user", "content": "hi"}},
		Tools:             []map[string]any{{"type": "function", "function": map[string]any{"name": "echo"}}},
		ToolChoice:        map[string]any{"type": "auto"},
		MaxTokens:         &maxTokens,
		Temperature:       &temp,
		TopP:              &topP,
		PresencePenalty:   &presence,
		FrequencyPenalty:  &frequency,
		Seed:              &seed,
		Stop:              []string{"DONE"},
		Logprobs:          &logprobs,
		TopLogprobs:       &topLogprobs,
		ParallelToolCalls: &parallel,
		Metadata:          map[string]any{"m": "v"},
		Provider:          map[string]any{"order": []any{"openai"}},
		Plugins:           []map[string]any{{"id": "auto-router"}},
		Route:             "fallback",
		Reasoning:         map[string]any{"effort": "medium"},
		SessionID:         "sess-1",
		Trace:             &trace,
		Modalities:        []string{"text"},
		ImageConfig:       map[string]any{"size": "1024x1024"},
		User:              "u-1",
	}

	body := tr.buildRequestBody(req)
	mustContain(t, body, "tool_choice")
	mustContain(t, body, "max_tokens")
	mustContain(t, body, "temperature")
	mustContain(t, body, "top_p")
	mustContain(t, body, "presence_penalty")
	mustContain(t, body, "frequency_penalty")
	mustContain(t, body, "seed")
	mustContain(t, body, "stop")
	mustContain(t, body, "logprobs")
	mustContain(t, body, "top_logprobs")
	mustContain(t, body, "parallel_tool_calls")
	mustContain(t, body, "provider")
	mustContain(t, body, "plugins")
	mustContain(t, body, "route")
	mustContain(t, body, "reasoning")
	mustContain(t, body, "session_id")
	mustContain(t, body, "trace")
	mustContain(t, body, "modalities")
	mustContain(t, body, "image_config")
	mustContain(t, body, "user")
	if _, ok := body["trace"].(map[string]any); !ok {
		t.Fatalf("expected trace object, got %#v", body["trace"])
	}
}

func TestBuildRequestBodyResponsesIncludesExpandedFields(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeResponses})
	maxOut := 120
	temp := 0.2
	topP := 0.9
	topK := 40.0
	trace := true
	parallel := true
	maxTools := 2
	bg := true
	store := false
	req := &config.ChatRequest{
		Model:              "openai/gpt-4o-mini",
		Messages:           []map[string]any{{"role": "user", "content": "hi"}},
		MaxOutputTokens:    &maxOut,
		Temperature:        &temp,
		TopP:               &topP,
		TopK:               &topK,
		ParallelToolCalls:  &parallel,
		Trace:              &trace,
		MaxToolCalls:       &maxTools,
		Instructions:       "answer shortly",
		PreviousResponseID: "resp_123",
		PromptCacheKey:     "cache_1",
		ServiceTier:        "auto",
		Truncation:         "auto",
		Include:            []string{"reasoning.encrypted_content"},
		Background:         &bg,
		SafetyIdentifier:   "safe-user",
		Store:              &store,
		Prompt:             map[string]any{"id": "prompt-1"},
		Route:              "fallback",
		Reasoning:          map[string]any{"effort": "high"},
		User:               "u-1",
	}

	body := tr.buildRequestBody(req)
	mustContain(t, body, "max_output_tokens")
	mustContain(t, body, "temperature")
	mustContain(t, body, "top_p")
	mustContain(t, body, "top_k")
	mustContain(t, body, "parallel_tool_calls")
	mustContain(t, body, "trace")
	mustContain(t, body, "max_tool_calls")
	mustContain(t, body, "instructions")
	mustContain(t, body, "previous_response_id")
	mustContain(t, body, "prompt_cache_key")
	mustContain(t, body, "service_tier")
	mustContain(t, body, "truncation")
	mustContain(t, body, "include")
	mustContain(t, body, "background")
	mustContain(t, body, "safety_identifier")
	mustContain(t, body, "store")
	mustContain(t, body, "prompt")
	mustContain(t, body, "reasoning")
	mustContain(t, body, "route")
	mustContain(t, body, "user")
	if _, ok := body["trace"].(map[string]any); !ok {
		t.Fatalf("expected trace object, got %#v", body["trace"])
	}
}

func TestBuildRequestBodyChatPreservesMultimodalInputBlocks(t *testing.T) {
	tr := NewHTTPTransport(&config.Options{OpenRouterAPIMode: config.OpenRouterAPIModeChatCompletions})
	req := &config.ChatRequest{
		Model: "openai/gpt-4o-mini",
		Messages: []map[string]any{
			{
				"role": "user",
				"content": []message.ContentBlock{
					&message.TextBlock{Type: message.BlockTypeText, Text: "Compare the attached assets."},
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
				},
			},
		},
	}

	body := tr.buildRequestBody(req)
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	jsonBody := string(raw)
	for _, needle := range []string{
		`"type":"image_url"`,
		`"image_url":{"url":"data:image/png;base64,aGVsbG8="}`,
		`"type":"file"`,
		`"file":{"filename":"spec.pdf","file_data":"data:application/pdf;base64,JVBERi0xLjQK"}`,
		`"type":"input_audio"`,
		`"input_audio":{"data":"UklGRg==","format":"wav"}`,
		`"type":"video_url"`,
		`"video_url":{"url":"https://example.com/demo.mp4"}`,
	} {
		if !strings.Contains(jsonBody, needle) {
			t.Fatalf("expected %q in request body: %s", needle, jsonBody)
		}
	}
}

func mustContain(t *testing.T, body map[string]any, key string) {
	t.Helper()
	if _, ok := body[key]; !ok {
		t.Fatalf("expected key %q in %#v", key, body)
	}
}
