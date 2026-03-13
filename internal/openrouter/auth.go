package openrouter

import (
	"errors"
	"os"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
)

// ResolveAPIKey resolves API key from options/env.
func ResolveAPIKey(opts *config.Options) (string, error) {
	if opts != nil && opts.APIKey != "" {
		return opts.APIKey, nil
	}
	if k := os.Getenv("OPENROUTER_API_KEY"); k != "" {
		return k, nil
	}
	return "", errors.New("missing OPENROUTER_API_KEY")
}
