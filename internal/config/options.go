package config

import (
	"log/slog"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/hook"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/mcp"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/permission"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/userinput"
)

// Effort controls thinking depth.
type Effort string

const (
	EffortLow    Effort = "low"
	EffortMedium Effort = "medium"
	EffortHigh   Effort = "high"
	EffortMax    Effort = "max"
)

// ThinkingConfig is a marker interface for thinking settings.
type ThinkingConfig interface{ thinkingConfig() }

// ThinkingConfigAdaptive enables adaptive thinking.
type ThinkingConfigAdaptive struct{}

func (ThinkingConfigAdaptive) thinkingConfig() {}

// ThinkingConfigEnabled enables thinking with a token budget.
type ThinkingConfigEnabled struct {
	BudgetTokens int
}

func (ThinkingConfigEnabled) thinkingConfig() {}

// ThinkingConfigDisabled disables thinking.
type ThinkingConfigDisabled struct{}

func (ThinkingConfigDisabled) thinkingConfig() {}

// OpenRouterAPIMode selects which OpenRouter API surface to use.
type OpenRouterAPIMode string

const (
	// OpenRouterAPIModeChatCompletions uses /chat/completions.
	OpenRouterAPIModeChatCompletions OpenRouterAPIMode = "chat_completions"
	// OpenRouterAPIModeResponses uses /responses.
	OpenRouterAPIModeResponses OpenRouterAPIMode = "responses"
)

// Options contains all SDK options.
type Options struct {
	Logger             *slog.Logger
	SystemPrompt       string
	SystemPromptPreset *SystemPromptPreset
	Model              string
	PermissionMode     string
	MaxTurns           int
	Cwd                string
	User               string

	Hooks                  map[hook.Event][]*hook.Matcher
	Thinking               ThinkingConfig
	Effort                 *Effort
	IncludePartialMessages bool
	MaxBudgetUSD           *float64

	MCPServers map[string]mcp.ServerConfig
	MCPConfig  string

	Tools           ToolsConfig
	AllowedTools    []string
	DisallowedTools []string
	CanUseTool      permission.Callback
	OnUserInput     userinput.Callback

	Resume           string
	ForkSession      bool
	SessionStorePath string

	FallbackModel            string
	PermissionPromptToolName string
	Plugins                  []*PluginConfig
	OutputFormat             map[string]any
	EnableFileCheckpointing  bool
	Transport                Transport

	// Observability
	MeterProvider        metric.MeterProvider
	TracerProvider       trace.TracerProvider
	PrometheusRegisterer prometheus.Registerer

	// OpenRouter specific
	APIKey            string
	BaseURL           string
	OpenRouterAPIMode OpenRouterAPIMode
	HTTPReferer       string
	XTitle            string
	RequestTimeout    *time.Duration
	MaxToolIterations int

	// OpenRouter request fields
	OpenRouterTopP               *float64
	OpenRouterTemperature        *float64
	OpenRouterMaxTokens          *int
	OpenRouterTopK               *float64
	OpenRouterPresencePenalty    *float64
	OpenRouterFrequencyPenalty   *float64
	OpenRouterSeed               *int64
	OpenRouterStop               []string
	OpenRouterLogprobs           *bool
	OpenRouterTopLogprobs        *int
	OpenRouterParallelToolCalls  *bool
	OpenRouterToolChoice         any
	OpenRouterProvider           map[string]any
	OpenRouterPlugins            []map[string]any
	OpenRouterRoute              string
	OpenRouterReasoning          map[string]any
	OpenRouterSessionID          string
	OpenRouterTrace              *bool
	OpenRouterModalities         []string
	OpenRouterImageConfig        map[string]any
	OpenRouterModels             []string
	OpenRouterMetadata           map[string]any
	OpenRouterInstructions       string
	OpenRouterPreviousResponseID string
	OpenRouterPromptCacheKey     string
	OpenRouterPrompt             map[string]any
	OpenRouterText               map[string]any
	OpenRouterMaxOutputTokens    *int
	OpenRouterMaxToolCalls       *int
	OpenRouterServiceTier        string
	OpenRouterTruncation         string
	OpenRouterInclude            []string
	OpenRouterBackground         *bool
	OpenRouterSafetyIdentifier   string
	OpenRouterStore              *bool
	OpenRouterExtra              map[string]any
}

// DefaultBaseURL is the default OpenRouter API base URL.
const DefaultBaseURL = "https://openrouter.ai/api/v1"

// ApplyDefaults fills missing option defaults.
func (o *Options) ApplyDefaults() {
	if o.PermissionMode == "" {
		o.PermissionMode = string(permission.ModeDefault)
	}
	if o.BaseURL == "" {
		o.BaseURL = DefaultBaseURL
	}
	if o.OpenRouterAPIMode == "" {
		o.OpenRouterAPIMode = OpenRouterAPIModeChatCompletions
	}
	if o.Model == "" {
		o.Model = "openai/gpt-4o-mini"
	}
	if o.MaxToolIterations == 0 {
		o.MaxToolIterations = 8
	}
}
