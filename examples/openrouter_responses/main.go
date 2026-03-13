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

	for msg, err := range sdk.Query(ctx, sdk.Text("List 3 testing best-practices for Go projects."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithOpenRouterAPIMode(sdk.OpenRouterAPIModeResponses),
		sdk.WithInstructions("Return concise bullets."),
		sdk.WithMaxOutputTokens(220),
		sdk.WithResponseText(map[string]any{"verbosity": "low"}),
		sdk.WithServiceTier("auto"),
		sdk.WithTruncation("auto"),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}
}
