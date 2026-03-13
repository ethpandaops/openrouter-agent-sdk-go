//go:build integration

package integration_test

import (
	"context"
	"sync/atomic"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestOnUserInput(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var invoked atomic.Bool
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts,
		openroutersdk.WithOnUserInput(func(_ context.Context, req *openroutersdk.UserInputRequest) (*openroutersdk.UserInputResponse, error) {
			invoked.Store(true)
			return &openroutersdk.UserInputResponse{
				Answers: map[string]*openroutersdk.UserInputAnswer{
					req.Questions[0].ID: {Answers: []string{"yes"}},
				},
			}, nil
		}),
		openroutersdk.WithToolChoice(map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "mcp__sdk__stdio"},
		}),
	)

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, opts...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Query(ctx, openrouterText(`Call the stdio tool to ask whether you should continue.`)); err != nil {
		t.Fatalf("query: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range client.ReceiveMessages(ctx) {
		}
	}()

	if err := waitForCondition(ctx, func() bool { return invoked.Load() }); err != nil {
		t.Fatalf("wait for user input callback: %v", err)
	}
	_ = client.Interrupt(ctx)
	<-done

	if !invoked.Load() {
		t.Fatal("expected OnUserInput callback to run")
	}
}
