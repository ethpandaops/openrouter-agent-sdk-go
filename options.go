package openroutersdk

import (
	"log/slog"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
)

// Option configures OpenRouterAgentOptions using the functional options pattern.
// This is the primary option type for configuring clients and queries.
type Option func(*OpenRouterAgentOptions)

// applyAgentOptions applies functional options to an OpenRouterAgentOptions struct.
func applyAgentOptions(opts []Option) *OpenRouterAgentOptions {
	options := &OpenRouterAgentOptions{}
	for _, opt := range opts {
		opt(options)
	}

	return options
}

// ===== Basic Configuration =====

// WithLogger sets the logger for debug output.
// If not set, logging is disabled (silent operation).
func WithLogger(logger *slog.Logger) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Logger = logger
	}
}

// WithSystemPrompt sets the system message to send to the model.
func WithSystemPrompt(prompt string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.SystemPrompt = prompt
	}
}

// WithSystemPromptPreset sets a preset system prompt configuration.
// If set, this takes precedence over WithSystemPrompt.
func WithSystemPromptPreset(preset *SystemPromptPreset) Option {
	return func(o *OpenRouterAgentOptions) {
		o.SystemPromptPreset = preset
	}
}

// WithModel specifies which OpenRouter model to use.
func WithModel(model string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Model = model
	}
}

// WithPermissionMode controls how permissions are handled.
// Valid values: "default", "acceptEdits", "plan", "bypassPermissions".
func WithPermissionMode(mode string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.PermissionMode = mode
	}
}

// WithMaxTurns limits the maximum number of conversation turns.
func WithMaxTurns(maxTurns int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.MaxTurns = maxTurns
	}
}

// WithCwd sets the working directory used by local session and tool features.
func WithCwd(cwd string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Cwd = cwd
	}
}

// WithUser sets a user identifier for tracking purposes.
func WithUser(user string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.User = user
	}
}

// ===== Hooks =====

// WithHooks configures event hooks for tool interception.
func WithHooks(hooks map[HookEvent][]*HookMatcher) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Hooks = hooks
	}
}

// ===== Token/Budget =====

// WithThinking sets the thinking configuration.
func WithThinking(thinking config.ThinkingConfig) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Thinking = thinking
	}
}

// WithEffort sets the thinking effort level.
func WithEffort(effort config.Effort) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Effort = &effort
	}
}

// WithIncludePartialMessages enables streaming of partial message updates.
func WithIncludePartialMessages(include bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.IncludePartialMessages = include
	}
}

// WithMaxBudgetUSD sets a cost limit for the session in USD.
func WithMaxBudgetUSD(budget float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.MaxBudgetUSD = &budget
	}
}

// ===== MCP =====

// WithMCPServers configures external MCP servers to connect to.
// Map key is the server name, value is the server configuration.
func WithMCPServers(servers map[string]MCPServerConfig) Option {
	return func(o *OpenRouterAgentOptions) {
		o.MCPServers = servers
	}
}

// WithMCPConfig sets a path to an MCP config file or a raw JSON string.
// If set, this takes precedence over WithMCPServers.
func WithMCPConfig(config string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.MCPConfig = config
	}
}

// ===== Tools =====

// WithTools specifies which tools are available.
// Accepts ToolsList (tool names) or *ToolsPreset.
func WithTools(tools config.ToolsConfig) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Tools = tools
	}
}

// WithAllowedTools sets pre-approved tools that can be used without prompting.
func WithAllowedTools(tools ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.AllowedTools = tools
	}
}

// WithDisallowedTools sets tools that are explicitly blocked.
func WithDisallowedTools(tools ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.DisallowedTools = tools
	}
}

// WithCanUseTool sets a callback for permission checking before each tool use.
func WithCanUseTool(callback ToolPermissionCallback) Option {
	return func(o *OpenRouterAgentOptions) {
		o.CanUseTool = callback
	}
}

// WithOnUserInput sets a callback for handling SDK user-input tool prompts.
func WithOnUserInput(callback UserInputCallback) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OnUserInput = callback
	}
}

// ===== Session =====

// WithResume sets a session ID to resume from.
func WithResume(sessionID string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Resume = sessionID
	}
}

// WithForkSession indicates whether to fork the resumed session to a new ID.
func WithForkSession(fork bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.ForkSession = fork
	}
}

// WithSessionStorePath enables durable session persistence at a JSON file path.
// When set, resume/fork state can survive process restarts.
func WithSessionStorePath(path string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.SessionStorePath = path
	}
}

// ===== Advanced =====

// WithFallbackModel specifies a model to use if the primary model fails.
func WithFallbackModel(model string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.FallbackModel = model
	}
}

// WithPermissionPromptToolName specifies the tool name to use for permission prompts.
func WithPermissionPromptToolName(name string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.PermissionPromptToolName = name
	}
}

// WithPlugins configures plugins to load.
func WithPlugins(plugins ...*SdkPluginConfig) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Plugins = plugins
	}
}

// WithOutputFormat specifies a JSON schema for structured output.
//
// The canonical format uses a wrapper object:
//
//	openroutersdk.WithOutputFormat(map[string]any{
//	    "type": "json_schema",
//	    "schema": map[string]any{
//	        "type":       "object",
//	        "properties": map[string]any{...},
//	        "required":   []string{...},
//	    },
//	})
//
// Raw JSON schemas (without the wrapper) are also accepted and auto-wrapped:
//
//	openroutersdk.WithOutputFormat(map[string]any{
//	    "type":       "object",
//	    "properties": map[string]any{...},
//	    "required":   []string{...},
//	})
//
// Structured output is available on [ResultMessage].StructuredOutput (parsed)
// or [ResultMessage].Result (JSON string).
func WithOutputFormat(format map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OutputFormat = format
	}
}

// WithEnableFileCheckpointing enables file change tracking and rewinding.
func WithEnableFileCheckpointing(enable bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.EnableFileCheckpointing = enable
	}
}

// WithSDKTools registers high-level Tool instances as an in-process MCP server.
// Tools are exposed under the "sdk" MCP server name (tool names: mcp__sdk__<name>).
// Each tool is automatically added to AllowedTools.
func WithSDKTools(tools ...Tool) Option {
	return func(o *OpenRouterAgentOptions) {
		if len(tools) == 0 {
			return
		}

		if o.MCPServers == nil {
			o.MCPServers = make(map[string]MCPServerConfig, 1)
		}
		o.MCPServers["sdk"] = createSDKToolServer(tools)
		for _, t := range tools {
			o.AllowedTools = append(o.AllowedTools, "mcp__sdk__"+t.Name())
		}
	}
}

// WithTransport injects a custom transport implementation.
// The transport must implement the Transport interface.
func WithTransport(transport Transport) Option {
	return func(o *OpenRouterAgentOptions) {
		o.Transport = transport
	}
}

// WithAPIKey sets the OpenRouter API key directly.
func WithAPIKey(apiKey string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.APIKey = apiKey
	}
}

// WithBaseURL overrides the OpenRouter base URL.
func WithBaseURL(baseURL string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.BaseURL = baseURL
	}
}

// WithOpenRouterAPIMode selects the OpenRouter API surface.
func WithOpenRouterAPIMode(mode config.OpenRouterAPIMode) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterAPIMode = mode
	}
}

// WithHTTPReferer sets the HTTP-Referer header for OpenRouter requests.
func WithHTTPReferer(referer string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.HTTPReferer = referer
	}
}

// WithXTitle sets the X-Title header for OpenRouter requests.
func WithXTitle(title string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.XTitle = title
	}
}

// WithRequestTimeout sets HTTP request timeout for OpenRouter calls.
func WithRequestTimeout(timeout time.Duration) Option {
	return func(o *OpenRouterAgentOptions) {
		o.RequestTimeout = &timeout
	}
}

// WithMaxToolIterations sets maximum tool-call loops per query.
func WithMaxToolIterations(max int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.MaxToolIterations = max
	}
}

// WithTemperature sets sampling temperature.
func WithTemperature(temperature float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTemperature = &temperature
	}
}

// WithMaxTokens sets chat max_tokens.
func WithMaxTokens(max int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterMaxTokens = &max
	}
}

// WithTopP sets nucleus sampling probability.
func WithTopP(topP float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTopP = &topP
	}
}

// WithTopK sets top-k sampling (responses API).
func WithTopK(topK float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTopK = &topK
	}
}

// WithPresencePenalty sets presence penalty.
func WithPresencePenalty(v float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterPresencePenalty = &v
	}
}

// WithFrequencyPenalty sets frequency penalty.
func WithFrequencyPenalty(v float64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterFrequencyPenalty = &v
	}
}

// WithSeed sets deterministic seed where supported.
func WithSeed(seed int64) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterSeed = &seed
	}
}

// WithStop sets stop sequences.
func WithStop(stop ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterStop = append([]string(nil), stop...)
	}
}

// WithLogprobs enables token log probabilities where supported.
func WithLogprobs(enable bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterLogprobs = &enable
	}
}

// WithTopLogprobs sets top_logprobs where supported.
func WithTopLogprobs(v int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTopLogprobs = &v
	}
}

// WithParallelToolCalls sets parallel_tool_calls.
func WithParallelToolCalls(enable bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterParallelToolCalls = &enable
	}
}

// WithToolChoice sets the tool_choice payload.
func WithToolChoice(choice any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterToolChoice = choice
	}
}

// WithProvider sets OpenRouter provider preferences.
func WithProvider(provider map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterProvider = provider
	}
}

// WithOpenRouterPlugins sets OpenRouter plugin payloads.
func WithOpenRouterPlugins(plugins ...map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterPlugins = append([]map[string]any(nil), plugins...)
	}
}

// WithRoute sets OpenRouter route preference.
func WithRoute(route string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterRoute = route
	}
}

// WithReasoning sets OpenRouter reasoning configuration.
func WithReasoning(reasoning map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterReasoning = reasoning
	}
}

// WithSessionID sets request session_id.
func WithSessionID(sessionID string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterSessionID = sessionID
	}
}

// WithTrace enables OpenRouter trace where supported.
func WithTrace(enable bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTrace = &enable
	}
}

// WithModalities sets output modalities.
func WithModalities(modalities ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterModalities = append([]string(nil), modalities...)
	}
}

// WithImageConfig sets provider-specific image config.
func WithImageConfig(cfg map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterImageConfig = cfg
	}
}

// WithModels sets candidate fallback models for OpenRouter routing.
func WithModels(models ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterModels = append([]string(nil), models...)
	}
}

// WithOpenRouterMetadata sets request metadata.
func WithOpenRouterMetadata(metadata map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterMetadata = metadata
	}
}

// WithInstructions sets responses.instructions.
func WithInstructions(instructions string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterInstructions = instructions
	}
}

// WithPreviousResponseID sets responses.previous_response_id.
func WithPreviousResponseID(responseID string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterPreviousResponseID = responseID
	}
}

// WithPromptCacheKey sets responses.prompt_cache_key.
func WithPromptCacheKey(key string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterPromptCacheKey = key
	}
}

// WithPrompt sets responses.prompt payload.
func WithPrompt(prompt map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterPrompt = prompt
	}
}

// WithResponseText sets responses.text payload directly.
func WithResponseText(text map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterText = text
	}
}

// WithMaxOutputTokens sets responses.max_output_tokens.
func WithMaxOutputTokens(max int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterMaxOutputTokens = &max
	}
}

// WithMaxToolCalls sets responses.max_tool_calls.
func WithMaxToolCalls(max int) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterMaxToolCalls = &max
	}
}

// WithServiceTier sets responses.service_tier.
func WithServiceTier(tier string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterServiceTier = tier
	}
}

// WithTruncation sets responses.truncation.
func WithTruncation(truncation string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterTruncation = truncation
	}
}

// WithInclude sets responses.include entries.
func WithInclude(include ...string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterInclude = append([]string(nil), include...)
	}
}

// WithBackground sets responses.background.
func WithBackground(background bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterBackground = &background
	}
}

// WithSafetyIdentifier sets responses.safety_identifier.
func WithSafetyIdentifier(safetyID string) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterSafetyIdentifier = safetyID
	}
}

// WithStore sets responses.store.
func WithStore(store bool) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterStore = &store
	}
}

// WithOpenRouterExtra merges raw request fields into the outgoing payload.
func WithOpenRouterExtra(extra map[string]any) Option {
	return func(o *OpenRouterAgentOptions) {
		o.OpenRouterExtra = extra
	}
}
