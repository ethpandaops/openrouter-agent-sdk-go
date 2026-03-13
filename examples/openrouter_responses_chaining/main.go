package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/ethpandaops/openrouter-agent-sdk-go"
	"github.com/ethpandaops/openrouter-agent-sdk-go/examples/internal/exampleutil"
)

func main() {
	if err := exampleutil.RequireAPIKey(); err != nil {
		exampleutil.PrintMissingAPIKeyHint()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	responseID, summaryText, err := runResponsesCall(ctx, "Summarize Go interfaces in 3 bullets.", "")
	if err != nil {
		fmt.Printf("first call error: %v\n", err)
		return
	}
	if responseID == "" {
		fmt.Println("warning: no response id detected; chaining call will still run without previous_response_id")
	}

	secondPrompt := "Now convert that summary into a 2-line elevator pitch."
	if strings.TrimSpace(summaryText) != "" {
		secondPrompt = fmt.Sprintf("Using this summary:\n%s\n\nConvert it into a 2-line elevator pitch.", summaryText)
	}

	_, _, err = runResponsesCall(ctx, secondPrompt, responseID)
	if err != nil {
		fmt.Printf("second call error: %v\n", err)
		return
	}
}

func runResponsesCall(ctx context.Context, prompt string, previousResponseID string) (string, string, error) {
	responseID := ""
	responseText := ""
	cacheKey := "examples-responses-chain"
	opts := []sdk.Option{
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithOpenRouterAPIMode(sdk.OpenRouterAPIModeResponses),
		sdk.WithPromptCacheKey(cacheKey),
		sdk.WithInclude("reasoning.encrypted_content"),
		sdk.WithBackground(false),
		sdk.WithSafetyIdentifier("examples-responses-chaining"),
		sdk.WithStore(false),
	}
	if strings.TrimSpace(previousResponseID) != "" {
		opts = append(opts, sdk.WithPreviousResponseID(previousResponseID))
	}

	for msg, err := range sdk.Query(ctx, sdk.Text(prompt), opts...) {
		if err != nil {
			return responseID, responseText, err
		}
		exampleutil.DisplayMessage(msg)
		if se, ok := msg.(*sdk.StreamEvent); ok {
			if id := extractResponseID(se.Event); id != "" {
				responseID = id
			}
		}
		if rm, ok := msg.(*sdk.ResultMessage); ok && rm.Result != nil {
			responseText = strings.TrimSpace(*rm.Result)
		}
	}
	return responseID, responseText, nil
}

func extractResponseID(event map[string]any) string {
	if id, ok := event["response_id"].(string); ok && strings.TrimSpace(id) != "" {
		return id
	}
	if id, ok := event["id"].(string); ok && strings.TrimSpace(id) != "" {
		if strings.HasPrefix(id, "resp_") {
			return id
		}
	}
	response, _ := event["response"].(map[string]any)
	if response == nil {
		return ""
	}
	if id, ok := response["id"].(string); ok {
		return strings.TrimSpace(id)
	}
	return ""
}
