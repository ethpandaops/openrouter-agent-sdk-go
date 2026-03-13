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

	for msg, err := range sdk.Query(ctx,
		sdk.Text(`Call the stdio tool to ask whether you should continue. If the answer is yes, reply exactly with confirmed.`),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithSystemPrompt("Use the stdio tool when instructed. Do not answer until the tool returns."),
		sdk.WithOnUserInput(func(_ context.Context, req *sdk.UserInputRequest) (*sdk.UserInputResponse, error) {
			fmt.Printf("User input requested: %s\n", req.Questions[0].Question)
			return &sdk.UserInputResponse{
				Answers: map[string]*sdk.UserInputAnswer{
					req.Questions[0].ID: {Answers: []string{"yes"}},
				},
			}, nil
		}),
		sdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__stdio"},
		}),
		sdk.WithMaxTurns(4),
		sdk.WithTemperature(0),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}
}
