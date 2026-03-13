package openroutersdk

import (
	"context"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
)

// ChatRequest is the normalized OpenRouter request sent through a Transport.
type ChatRequest = config.ChatRequest

// Transport defines the runtime transport interface.
type Transport interface {
	Start(ctx context.Context) error
	CreateStream(ctx context.Context, req *ChatRequest) (<-chan map[string]any, <-chan error)
	Close() error
}
