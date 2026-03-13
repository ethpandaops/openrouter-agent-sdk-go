package openroutersdk

import (
	"context"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/openrouter"
)

// ListModels returns the available OpenRouter models.
func ListModels(ctx context.Context, opts ...Option) ([]ModelInfo, error) {
	resp, err := ListModelsResponse(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return resp.Models, nil
}

// ListModelsResponse returns the full OpenRouter model discovery payload.
func ListModelsResponse(ctx context.Context, opts ...Option) (*ModelListResponse, error) {
	cfg := applyAgentOptions(opts)
	if cfg == nil {
		cfg = &config.Options{}
	}
	cfg.ApplyDefaults()
	return openrouter.ListModelsResponse(ctx, cfg)
}
