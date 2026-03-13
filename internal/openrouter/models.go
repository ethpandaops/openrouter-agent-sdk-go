package openrouter

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/model"
)

type modelsEnvelope struct {
	Data []json.RawMessage `json:"data"`
}

type rawModel struct {
	ID                  string                  `json:"id"`
	Name                string                  `json:"name"`
	CanonicalSlug       string                  `json:"canonical_slug"`
	Description         string                  `json:"description"`
	ContextLength       int                     `json:"context_length"`
	Architecture        *model.Architecture     `json:"architecture"`
	Pricing             *model.Pricing          `json:"pricing"`
	TopProvider         *model.TopProvider      `json:"top_provider"`
	PerRequestLimits    *model.PerRequestLimits `json:"per_request_limits"`
	SupportedParameters []string                `json:"supported_parameters"`
	Endpoints           []model.Endpoint        `json:"endpoints"`
	DefaultEndpoint     string                  `json:"default_endpoint"`
	IsModerated         bool                    `json:"is_moderated"`
}

// ListModelsResponse queries the OpenRouter models endpoint.
func ListModelsResponse(ctx context.Context, opts *config.Options) (*model.ListResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts == nil {
		opts = &config.Options{}
	}
	opts.ApplyDefaults()

	timeout := 60 * time.Second
	if opts.RequestTimeout != nil {
		timeout = *opts.RequestTimeout
	}
	client := &http.Client{Timeout: timeout}

	baseURL := opts.BaseURL
	if strings.TrimSpace(baseURL) == "" {
		baseURL = config.DefaultBaseURL
	}

	apiKey, authErr := ResolveAPIKey(opts)
	if authErr == nil {
		resp, status, err := listModelsRequest(ctx, client, baseURL, "models/user", apiKey)
		if err == nil {
			resp.Authenticated = true
			resp.Endpoint = "models/user"
			return resp, nil
		}
		if status != http.StatusUnauthorized && status != http.StatusForbidden && status != http.StatusNotFound {
			return nil, err
		}
	}

	resp, _, err := listModelsRequest(ctx, client, baseURL, "models", apiKey)
	if err != nil {
		return nil, err
	}
	resp.Authenticated = false
	resp.Endpoint = "models"
	return resp, nil
}

func listModelsRequest(
	ctx context.Context,
	client *http.Client,
	baseURL string,
	path string,
	apiKey string,
) (*model.ListResponse, int, error) {
	endpointURL, err := url.JoinPath(strings.TrimSuffix(baseURL, "/"), path)
	if err != nil {
		return nil, 0, fmt.Errorf("join model endpoint path: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("build model request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "openrouter-agent-sdk-go/0.1.0")
	if strings.TrimSpace(apiKey) != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("execute model request: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, res.StatusCode, fmt.Errorf("read model response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, res.StatusCode, fmt.Errorf("openrouter model listing failed status=%d body=%s", res.StatusCode, string(body))
	}

	var envelope modelsEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, res.StatusCode, fmt.Errorf("decode model response: %w", err)
	}

	out := &model.ListResponse{
		Object:  "list",
		Source:  "openrouter",
		RawData: make([]model.Info, 0, len(envelope.Data)),
		Models:  make([]model.Info, 0, len(envelope.Data)),
	}

	for _, item := range envelope.Data {
		info, err := decodeModel(item)
		if err != nil {
			return nil, res.StatusCode, err
		}
		out.RawData = append(out.RawData, info)
		out.Models = append(out.Models, info)
	}
	out.Total = len(out.Models)
	return out, res.StatusCode, nil
}

func decodeModel(raw json.RawMessage) (model.Info, error) {
	var base rawModel
	if err := json.Unmarshal(raw, &base); err != nil {
		return model.Info{}, fmt.Errorf("decode model entry: %w", err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return model.Info{}, fmt.Errorf("decode model metadata: %w", err)
	}
	delete(metadata, "id")
	delete(metadata, "name")
	delete(metadata, "canonical_slug")
	delete(metadata, "description")
	delete(metadata, "context_length")
	delete(metadata, "architecture")
	delete(metadata, "pricing")
	delete(metadata, "top_provider")
	delete(metadata, "per_request_limits")
	delete(metadata, "supported_parameters")
	delete(metadata, "endpoints")
	delete(metadata, "default_endpoint")
	delete(metadata, "is_moderated")

	info := model.Info{
		ID:                  base.ID,
		Name:                base.Name,
		CanonicalSlug:       base.CanonicalSlug,
		Description:         base.Description,
		ContextLength:       base.ContextLength,
		Architecture:        base.Architecture,
		Pricing:             base.Pricing,
		TopProvider:         base.TopProvider,
		PerRequestLimits:    base.PerRequestLimits,
		SupportedParameters: append(model.SupportedParameters(nil), base.SupportedParameters...),
		Endpoints:           append([]model.Endpoint(nil), base.Endpoints...),
		DefaultEndpoint:     base.DefaultEndpoint,
		IsModerated:         base.IsModerated,
		IsReasoning:         looksReasoningModel(base),
		Metadata:            metadata,
	}
	info.IsFree = info.CostTier() == "free"
	return info, nil
}

func looksReasoningModel(in rawModel) bool {
	for _, p := range in.SupportedParameters {
		if p == "reasoning" {
			return true
		}
	}
	name := strings.ToLower(in.ID + " " + in.Name)
	return strings.Contains(name, "reason") || strings.Contains(name, "o1") || strings.Contains(name, "o3") || strings.Contains(name, "r1")
}
