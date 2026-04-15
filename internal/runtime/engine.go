package runtime

import (
	"log/slog"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/observability"
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

	obs := resolveObserver(opts)

	// Inject observer into the HTTP transport if it's the default type.
	if ht, ok := opts.Transport.(*openrouter.HTTPTransport); ok {
		ht.SetObserver(obs)
	}

	// Inject observer into the session manager.
	if sessions != nil {
		sessions.SetObserver(obs)
	}

	return &Engine{runner: NewQueryRunner(opts, sessions, obs)}
}

// Runner returns the query runner.
func (e *Engine) Runner() *QueryRunner { return e.runner }

// resolveObserver creates the appropriate Observer based on config options.
// When PrometheusRegisterer is set and MeterProvider is not, it creates an
// OTel-to-Prometheus bridge automatically.
func resolveObserver(opts *config.Options) *observability.Observer {
	if opts == nil {
		return observability.Noop()
	}

	cfg := observability.Config{
		MeterProvider:  opts.MeterProvider,
		TracerProvider: opts.TracerProvider,
	}

	// If no MeterProvider but PrometheusRegisterer is set, bridge it.
	if cfg.MeterProvider == nil && opts.PrometheusRegisterer != nil {
		mp, err := observability.NewPrometheusMeterProvider(opts.PrometheusRegisterer)
		if err != nil {
			if opts.Logger != nil {
				opts.Logger.Warn("failed to create prometheus meter provider, falling back to noop",
					slog.String("error", err.Error()),
				)
			}
		} else {
			cfg.MeterProvider = mp
		}
	}

	return observability.New(cfg)
}
