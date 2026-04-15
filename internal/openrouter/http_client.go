package openrouter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/observability"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/util"
)

// HTTPTransport implements config.Transport against OpenRouter chat completions.
type HTTPTransport struct {
	opts    *config.Options
	client  *http.Client
	apiKey  string
	baseURL string
	obs     *observability.Observer

	mu      sync.Mutex
	started bool
}

// NewHTTPTransport creates an OpenRouter HTTP transport.
func NewHTTPTransport(opts *config.Options) *HTTPTransport {
	timeout := 60 * time.Second
	if opts != nil && opts.RequestTimeout != nil {
		timeout = *opts.RequestTimeout
	}
	return &HTTPTransport{
		opts: opts,
		obs:  observability.Noop(),
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// SetObserver sets the observability observer for the transport.
func (t *HTTPTransport) SetObserver(obs *observability.Observer) {
	if obs != nil {
		t.obs = obs
	}
}

// Start initializes transport state.
func (t *HTTPTransport) Start(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.started {
		return nil
	}
	key, err := ResolveAPIKey(t.opts)
	if err != nil {
		return err
	}
	t.apiKey = key
	if t.opts != nil && t.opts.BaseURL != "" {
		t.baseURL = t.opts.BaseURL
	} else {
		t.baseURL = config.DefaultBaseURL
	}
	t.started = true
	return nil
}

// CreateStream creates a streaming completion request.
func (t *HTTPTransport) CreateStream(ctx context.Context, req *config.ChatRequest) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, 32)
	errs := make(chan error, 4)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				select {
				case errs <- fmt.Errorf("panic in CreateStream: %v", r):
				default:
				}
				close(out)
				close(errs)
			}
		}()

		if err := t.Start(ctx); err != nil {
			errs <- err
			close(out)
			close(errs)
			return
		}

		body := t.buildRequestBody(req)

		payload, err := json.Marshal(body)
		if err != nil {
			errs <- fmt.Errorf("marshal request: %w", err)
			close(out)
			close(errs)
			return
		}

		resp, err := t.doRequest(ctx, payload)
		if err != nil {
			errs <- err
			close(out)
			close(errs)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		ct := strings.ToLower(resp.Header.Get("content-type"))
		if !strings.Contains(ct, "text/event-stream") {
			raw, _ := io.ReadAll(resp.Body)
			errs <- fmt.Errorf("expected sse response, got %s: %s", ct, string(raw))
			close(out)
			close(errs)
			return
		}

		util.ParseSSE(ctx, resp.Body, out, errs)
	}()

	return out, errs
}

func (t *HTTPTransport) doRequest(ctx context.Context, payload []byte) (*http.Response, error) {
	endpoint := t.endpointPath()
	u, err := url.JoinPath(strings.TrimSuffix(t.baseURL, "/"), endpoint)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		reqCtx, reqSpan := t.obs.StartHTTPSpan(ctx, http.MethodPost, endpoint)

		req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, u, bytes.NewReader(payload))
		if err != nil {
			reqSpan.End()
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("User-Agent", "openrouter-agent-sdk-go/0.1.0")
		if t.opts != nil {
			if t.opts.HTTPReferer != "" {
				req.Header.Set("HTTP-Referer", t.opts.HTTPReferer)
			}
			if t.opts.XTitle != "" {
				req.Header.Set("X-Title", t.opts.XTitle)
			}
		}

		isRetry := attempt > 0
		resp, err := t.client.Do(req)
		if err != nil {
			lastErr = err
			reqSpan.End()
		} else {
			sc := observability.StatusClass(resp.StatusCode)
			t.obs.RecordHTTPRequest(ctx, sc, isRetry)

			if resp.StatusCode == 429 {
				t.obs.RecordRateLimitEvent(ctx)
			}

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				reqSpan.End()
				return resp, nil
			}
			raw, _ := io.ReadAll(resp.Body)
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("openrouter error status=%d body=%s", resp.StatusCode, string(raw))
			reqSpan.End()
			if resp.StatusCode < 500 && resp.StatusCode != 429 {
				return nil, lastErr
			}
		}

		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(util.Backoff(attempt)):
			}
		}
	}

	return nil, lastErr
}

func (t *HTTPTransport) endpointPath() string {
	if t.opts != nil && t.opts.OpenRouterAPIMode == config.OpenRouterAPIModeResponses {
		return "responses"
	}
	return "chat/completions"
}

func (t *HTTPTransport) buildRequestBody(req *config.ChatRequest) map[string]any {
	mode := config.OpenRouterAPIModeChatCompletions
	if t.opts != nil && t.opts.OpenRouterAPIMode != "" {
		mode = t.opts.OpenRouterAPIMode
	}

	if mode == config.OpenRouterAPIModeResponses {
		body := map[string]any{
			"model":  req.Model,
			"input":  req.Messages,
			"stream": true,
		}
		if len(req.Models) > 0 {
			body["models"] = cloneStringSlice(req.Models)
		}
		if len(req.Tools) > 0 {
			body["tools"] = req.Tools
			if req.ToolChoice != nil {
				body["tool_choice"] = req.ToolChoice
			} else {
				body["tool_choice"] = "auto"
			}
		} else if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		}
		if req.MaxOutputTokens != nil {
			body["max_output_tokens"] = *req.MaxOutputTokens
		} else if req.MaxTokens != nil {
			body["max_output_tokens"] = *req.MaxTokens
		}
		if req.Temperature != nil {
			body["temperature"] = *req.Temperature
		}
		if req.TopP != nil {
			body["top_p"] = *req.TopP
		}
		if req.TopK != nil {
			body["top_k"] = *req.TopK
		}
		if req.PresencePenalty != nil {
			body["presence_penalty"] = *req.PresencePenalty
		}
		if req.FrequencyPenalty != nil {
			body["frequency_penalty"] = *req.FrequencyPenalty
		}
		if req.ParallelToolCalls != nil {
			body["parallel_tool_calls"] = *req.ParallelToolCalls
		}
		if req.TopLogprobs != nil {
			body["top_logprobs"] = *req.TopLogprobs
		}
		if req.Logprobs != nil && *req.Logprobs && req.TopLogprobs == nil {
			body["top_logprobs"] = 1
		}
		if req.MaxToolCalls != nil {
			body["max_tool_calls"] = *req.MaxToolCalls
		}
		if req.Instructions != "" {
			body["instructions"] = req.Instructions
		}
		if req.PreviousResponseID != "" {
			body["previous_response_id"] = req.PreviousResponseID
		}
		if req.PromptCacheKey != "" {
			body["prompt_cache_key"] = req.PromptCacheKey
		}
		if req.ServiceTier != "" {
			body["service_tier"] = req.ServiceTier
		}
		if req.Truncation != "" {
			body["truncation"] = req.Truncation
		}
		if len(req.Include) > 0 {
			body["include"] = cloneStringSlice(req.Include)
		}
		if req.Background != nil {
			body["background"] = *req.Background
		}
		if req.SafetyIdentifier != "" {
			body["safety_identifier"] = req.SafetyIdentifier
		}
		if req.Store != nil {
			body["store"] = *req.Store
		}
		if len(req.Prompt) > 0 {
			body["prompt"] = cloneMap(req.Prompt)
		}
		if len(req.ResponseFormat) > 0 {
			text := cloneMap(req.ResponseText)
			if text == nil {
				text = map[string]any{}
			}
			text["format"] = normalizeResponsesTextFormat(req.ResponseFormat)
			body["text"] = text
		} else if len(req.ResponseText) > 0 {
			body["text"] = cloneMap(req.ResponseText)
		}
		if len(req.Metadata) > 0 {
			body["metadata"] = cloneMap(req.Metadata)
		}
		if len(req.Provider) > 0 {
			body["provider"] = cloneMap(req.Provider)
		}
		if len(req.Plugins) > 0 {
			body["plugins"] = cloneMapSlice(req.Plugins)
		}
		if req.Route != "" {
			body["route"] = req.Route
		}
		if len(req.Reasoning) > 0 {
			body["reasoning"] = cloneMap(req.Reasoning)
		}
		if req.SessionID != "" {
			body["session_id"] = req.SessionID
		}
		if req.Trace != nil && *req.Trace {
			body["trace"] = map[string]any{}
		}
		if len(req.Modalities) > 0 {
			body["modalities"] = cloneStringSlice(req.Modalities)
		}
		if len(req.ImageConfig) > 0 {
			body["image_config"] = cloneMap(req.ImageConfig)
		}
		if req.User != "" {
			body["user"] = req.User
		}
		if len(req.Extra) > 0 {
			mergeInto(body, req.Extra)
		}
		return body
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	if len(req.Models) > 0 {
		body["models"] = cloneStringSlice(req.Models)
	}
	if len(req.Tools) > 0 {
		body["tools"] = req.Tools
		if req.ToolChoice != nil {
			body["tool_choice"] = req.ToolChoice
		} else {
			body["tool_choice"] = "auto"
		}
	} else if req.ToolChoice != nil {
		body["tool_choice"] = req.ToolChoice
	}
	if req.MaxTokens != nil {
		body["max_tokens"] = *req.MaxTokens
	} else if req.MaxOutputTokens != nil {
		body["max_tokens"] = *req.MaxOutputTokens
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}
	if req.TopP != nil {
		body["top_p"] = *req.TopP
	}
	if req.PresencePenalty != nil {
		body["presence_penalty"] = *req.PresencePenalty
	}
	if req.FrequencyPenalty != nil {
		body["frequency_penalty"] = *req.FrequencyPenalty
	}
	if req.Seed != nil {
		body["seed"] = *req.Seed
	}
	if len(req.Stop) > 0 {
		if len(req.Stop) == 1 {
			body["stop"] = req.Stop[0]
		} else {
			body["stop"] = cloneStringSlice(req.Stop)
		}
	}
	if req.Logprobs != nil {
		body["logprobs"] = *req.Logprobs
	}
	if req.TopLogprobs != nil {
		body["top_logprobs"] = *req.TopLogprobs
	}
	if req.ParallelToolCalls != nil {
		body["parallel_tool_calls"] = *req.ParallelToolCalls
	}
	if len(req.ResponseFormat) > 0 {
		body["response_format"] = normalizeChatResponseFormat(req.ResponseFormat)
	}
	if len(req.Metadata) > 0 {
		body["metadata"] = cloneMap(req.Metadata)
	}
	if len(req.Provider) > 0 {
		body["provider"] = cloneMap(req.Provider)
	}
	if len(req.Plugins) > 0 {
		body["plugins"] = cloneMapSlice(req.Plugins)
	}
	if req.Route != "" {
		body["route"] = req.Route
	}
	if len(req.Reasoning) > 0 {
		body["reasoning"] = cloneMap(req.Reasoning)
	}
	if req.SessionID != "" {
		body["session_id"] = req.SessionID
	}
	if req.Trace != nil && *req.Trace {
		body["trace"] = map[string]any{}
	}
	if len(req.Modalities) > 0 {
		body["modalities"] = cloneStringSlice(req.Modalities)
	}
	if len(req.ImageConfig) > 0 {
		body["image_config"] = cloneMap(req.ImageConfig)
	}
	if req.User != "" {
		body["user"] = req.User
	}
	if len(req.Extra) > 0 {
		mergeInto(body, req.Extra)
	}
	return body
}

func normalizeChatResponseFormat(format map[string]any) map[string]any {
	if len(format) == 0 {
		return nil
	}
	formatType, _ := format["type"].(string)
	if formatType == "json_schema" {
		if _, ok := format["json_schema"]; ok {
			return cloneMap(format)
		}
		if schema, ok := format["schema"].(map[string]any); ok {
			out := cloneMap(format)
			delete(out, "schema")
			out["json_schema"] = buildJSONSchemaConfig(schema)
			return out
		}
		return cloneMap(format)
	}
	if schema, ok := format["json_schema"].(map[string]any); ok && len(schema) > 0 {
		return map[string]any{
			"type":        "json_schema",
			"json_schema": buildJSONSchemaConfig(schema),
		}
	}
	if schema, ok := format["schema"].(map[string]any); ok && len(schema) > 0 {
		return map[string]any{
			"type":        "json_schema",
			"json_schema": buildJSONSchemaConfig(schema),
		}
	}
	if _, hasProperties := format["properties"]; hasProperties || format["type"] == "object" {
		return map[string]any{
			"type":        "json_schema",
			"json_schema": buildJSONSchemaConfig(format),
		}
	}
	return cloneMap(format)
}

func normalizeResponsesTextFormat(format map[string]any) map[string]any {
	if len(format) == 0 {
		return nil
	}
	formatType, _ := format["type"].(string)
	if formatType == "json_schema" {
		if schema, ok := format["schema"].(map[string]any); ok {
			name, _ := format["name"].(string)
			if strings.TrimSpace(name) == "" {
				name = "response"
			}
			out := map[string]any{
				"type":   "json_schema",
				"name":   name,
				"schema": cloneMap(schema),
			}
			if desc, ok := format["description"].(string); ok {
				out["description"] = desc
			}
			if strict, ok := format["strict"].(bool); ok {
				out["strict"] = strict
			}
			return out
		}
		if js, ok := format["json_schema"].(map[string]any); ok {
			name, _ := js["name"].(string)
			if strings.TrimSpace(name) == "" {
				name = "response"
			}
			schema, _ := js["schema"].(map[string]any)
			out := map[string]any{
				"type":   "json_schema",
				"name":   name,
				"schema": cloneMap(schema),
			}
			if desc, ok := js["description"].(string); ok {
				out["description"] = desc
			}
			if strict, ok := js["strict"].(bool); ok {
				out["strict"] = strict
			}
			return out
		}
		return map[string]any{
			"type":   "json_schema",
			"name":   "response",
			"schema": map[string]any{},
		}
	}
	if _, hasProperties := format["properties"]; hasProperties || format["type"] == "object" {
		return map[string]any{
			"type":   "json_schema",
			"name":   "response",
			"schema": cloneMap(format),
		}
	}
	return cloneMap(format)
}

func buildJSONSchemaConfig(schema map[string]any) map[string]any {
	if len(schema) == 0 {
		return map[string]any{"name": "response"}
	}
	if inner, ok := schema["schema"].(map[string]any); ok {
		out := cloneMap(schema)
		out["schema"] = cloneMap(inner)
		if name, ok := out["name"].(string); !ok || strings.TrimSpace(name) == "" {
			out["name"] = "response"
		}
		return out
	}
	return map[string]any{
		"name":   "response",
		"schema": cloneMap(schema),
	}
}

func cloneMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneMapSlice(values []map[string]any) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, v := range values {
		out = append(out, cloneMap(v))
	}
	return out
}

func cloneStringSlice(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}

func mergeInto(dst map[string]any, extra map[string]any) {
	for k, v := range extra {
		dst[k] = v
	}
}

// Close closes transport resources.
func (t *HTTPTransport) Close() error { return nil }
