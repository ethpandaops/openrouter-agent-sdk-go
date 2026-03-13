//go:build integration

package integration_test

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestQueryWithLogger(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	logger := slog.Default()
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithLogger(logger))

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Say hello."), opts...))
	// collectResult guarantees a non-nil ResultMessage was received;
	// Result text may be nil for some models (e.g. empty assistant turn) so just
	// verify the query completed without error.
	_ = result
}

func TestQueryContextTimeout(t *testing.T) {
	// Call integrationContext to check API key, then discard.
	_, cancel := integrationContext(t)
	cancel()

	shortCtx, shortCancel := context.WithTimeout(context.Background(), 1)
	defer shortCancel()

	var gotErr error
	for _, err := range openroutersdk.Query(shortCtx, openrouterText("Write a very long essay."), integrationOptions()...) {
		if err != nil {
			gotErr = err
			break
		}
	}

	if gotErr == nil {
		t.Fatal("expected timeout error")
	}
}

func TestQueryMultiTurn(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	client := openroutersdk.NewClient()
	if err := client.Start(ctx, integrationOptions()...); err != nil {
		t.Fatalf("start client: %v", err)
	}
	defer func() { _ = client.Close() }()

	// Turn 1: establish context.
	if err := client.Query(ctx, openrouterText("Remember the number 42. Reply with just 'remembered'.")); err != nil {
		t.Fatalf("query turn 1: %v", err)
	}

	var turn1Done bool
	for msg, err := range client.ReceiveResponse(ctx) {
		if err != nil {
			t.Fatalf("receive turn 1: %v", err)
		}
		if _, ok := msg.(*openroutersdk.ResultMessage); ok {
			turn1Done = true
		}
	}

	if !turn1Done {
		t.Fatal("expected result from turn 1")
	}

	// Turn 2: recall context.
	if err := client.Query(ctx, openrouterText("What number did I ask you to remember? Reply with just the number.")); err != nil {
		t.Fatalf("query turn 2: %v", err)
	}

	var sawAnswer bool
	for msg, err := range client.ReceiveResponse(ctx) {
		if err != nil {
			t.Fatalf("receive turn 2: %v", err)
		}
		switch m := msg.(type) {
		case *openroutersdk.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*openroutersdk.TextBlock); ok {
					if strings.Contains(tb.Text, "42") {
						sawAnswer = true
					}
				}
			}
		case *openroutersdk.ResultMessage:
			if m.Result != nil && strings.Contains(*m.Result, "42") {
				sawAnswer = true
			}
		}
	}

	if !sawAnswer {
		t.Fatal("expected model to recall 42 in turn 2")
	}
}

func TestQueryMessageTypes(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var (
		sawAssistant bool
		sawResult    bool
	)

	for msg, err := range openroutersdk.Query(ctx, openrouterText("Reply with the word: hello."), integrationOptions()...) {
		if err != nil {
			t.Fatalf("query error: %v", err)
		}

		switch m := msg.(type) {
		case *openroutersdk.AssistantMessage:
			sawAssistant = true
		case *openroutersdk.ResultMessage:
			sawResult = true
			if m.DurationMs <= 0 {
				t.Fatalf("expected positive duration, got %d", m.DurationMs)
			}
			if m.NumTurns <= 0 {
				t.Fatalf("expected positive num_turns, got %d", m.NumTurns)
			}
		}
	}

	if !sawAssistant {
		t.Fatal("expected assistant message")
	}
	if !sawResult {
		t.Fatal("expected result message")
	}
}

func TestQuerySystemPrompt(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithSystemPrompt(
		"You are a pirate. Always respond with 'Arrr!' at the start of every response.",
	))

	var sawPirate bool
	for msg, err := range openroutersdk.Query(ctx, openrouterText("Say hello."), opts...) {
		if err != nil {
			t.Fatalf("query error: %v", err)
		}

		switch m := msg.(type) {
		case *openroutersdk.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*openroutersdk.TextBlock); ok {
					if strings.Contains(strings.ToLower(tb.Text), "arrr") {
						sawPirate = true
					}
				}
			}
		case *openroutersdk.ResultMessage:
			if m.Result != nil && strings.Contains(strings.ToLower(*m.Result), "arrr") {
				sawPirate = true
			}
		}
	}

	if !sawPirate {
		t.Fatal("expected pirate-style response with system prompt")
	}
}
