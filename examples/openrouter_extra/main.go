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

	for msg, err := range sdk.Query(ctx, sdk.Text("Give me one deployment checklist item."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithOpenRouterExtra(map[string]any{
			"metadata": map[string]any{
				"example": "openrouter_extra",
				"mode":    "escape_hatch",
			},
			"provider": map[string]any{
				"allow_fallbacks": true,
			},
		}),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}
}
