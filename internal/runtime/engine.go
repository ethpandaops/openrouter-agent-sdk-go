package runtime

import (
	"log/slog"

	agenterrclass "github.com/ethpandaops/agent-sdk-observability/errclass"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/controlplane"
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
	// Both are expected control-flow conditions, mapped to InvalidRequest to
	// keep the error.type label set closed.
	obs.RegisterSentinel(session.ErrNoCheckpoint, agenterrclass.InvalidRequest)
	obs.RegisterSentinel(controlplane.ErrUnsupportedControl, agenterrclass.InvalidRequest)

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

// resolveObserver creates the Observer based on config options. On any
// construction error it logs (when a logger is configured) and falls back to
// a noop Observer so NewEngine stays infallible. Mirrors the existing
// Prometheus-bridge fallback below.
func resolveObserver(opts *config.Options) *observability.Observer {
	if opts == nil {
		return observability.Noop()
	}

	cfg := observability.Config{
		MeterProvider:  opts.MeterProvider,
		TracerProvider: opts.TracerProvider,
		Logger:         opts.Logger,
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

	obs, err := observability.New(cfg)
	if err != nil {
		if opts.Logger != nil {
			opts.Logger.Warn("observability disabled, falling back to noop",
				slog.String("error", err.Error()),
			)
		}
		return observability.Noop()
	}
	return obs
}
