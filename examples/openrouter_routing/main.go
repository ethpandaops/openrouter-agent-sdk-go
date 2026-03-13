package main

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/ethpandaops/openrouter-agent-sdk-go"
	"github.com/ethpandaops/openrouter-agent-sdk-go/examples/internal/exampleutil"
)

func main() {
	if err := exampleutil.RequireAPIKey(); err != nil {
		exampleutil.PrintMissingAPIKeyHint()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	for msg, err := range sdk.Query(ctx, sdk.Text("Explain in one paragraph when to use buffered channels."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithModels(exampleutil.DefaultModel(), "google/gemma-3-4b-it:free"),
		sdk.WithProvider(map[string]any{"allow_fallbacks": true}),
		sdk.WithOpenRouterPlugins(map[string]any{"id": "auto-router"}),
		sdk.WithRoute("fallback"),
		sdk.WithSessionID("examples-routing-session"),
		sdk.WithTrace(true),
		sdk.WithOpenRouterMetadata(map[string]any{"example": "openrouter_routing"}),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}
}
