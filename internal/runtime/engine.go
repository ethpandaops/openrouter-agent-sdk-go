package runtime

import (
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/openrouter"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
)

// Engine owns runtime dependencies used by Query and Client.
type Engine struct {
	runner *QueryRunner
}

// NewEngine creates a new runtime engine.
func NewEngine(opts *config.Options, sessions *session.Manager) *Engine {
	if opts.Transport == nil {
		opts.Transport = openrouter.NewHTTPTransport(opts)
	}
	return &Engine{runner: NewQueryRunner(opts, sessions)}
}

// Runner returns the query runner.
func (e *Engine) Runner() *QueryRunner { return e.runner }
