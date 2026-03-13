package config

import "context"

// ChatRequest is the normalized OpenRouter chat request used by transports.
type ChatRequest struct {
	Model              string
	Models             []string
	Messages           []map[string]any
	Tools              []map[string]any
	Stream             bool
	ToolChoice         any
	MaxTokens          *int
	MaxOutputTokens    *int
	Temperature        *float64
	TopP               *float64
	TopK               *float64
	PresencePenalty    *float64
	FrequencyPenalty   *float64
	Seed               *int64
	Stop               []string
	Logprobs           *bool
	TopLogprobs        *int
	ParallelToolCalls  *bool
	ResponseFormat     map[string]any
	ResponseText       map[string]any
	Metadata           map[string]any
	Provider           map[string]any
	Plugins            []map[string]any
	Route              string
	Reasoning          map[string]any
	SessionID          string
	Trace              *bool
	Modalities         []string
	ImageConfig        map[string]any
	User               string
	Instructions       string
	PreviousResponseID string
	PromptCacheKey     string
	MaxToolCalls       *int
	ServiceTier        string
	Truncation         string
	Include            []string
	Background         *bool
	SafetyIdentifier   string
	Store              *bool
	Prompt             map[string]any
	Extra              map[string]any
}

// Transport defines the runtime transport interface.
type Transport interface {
	Start(ctx context.Context) error
	CreateStream(ctx context.Context, req *ChatRequest) (<-chan map[string]any, <-chan error)
	Close() error
}
