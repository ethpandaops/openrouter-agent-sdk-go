//go:build integration

package integration_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

const (
	// defaultIntegrationModel keeps routine integration runs on the cheap free router.
	// Set OPENROUTER_MODEL to a pinned tool-capable model when stricter coverage matters.
	defaultIntegrationModel = "openrouter/free"
)

func integrationContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	if os.Getenv("OPENROUTER_API_KEY") == "" {
		t.Skip("OPENROUTER_API_KEY is required for integration tests")
	}
	return context.WithTimeout(context.Background(), 90*time.Second)
}

func integrationOptions() []openroutersdk.Option {
	model := os.Getenv("OPENROUTER_MODEL")
	if model == "" {
		model = defaultIntegrationModel
	}
	return []openroutersdk.Option{
		openroutersdk.WithModel(model),
		openroutersdk.WithMaxTurns(6),
		openroutersdk.WithTemperature(0),
	}
}

func openrouterText(text string) openroutersdk.UserMessageContent {
	return openroutersdk.Text(text)
}

func integrationResponsesOptions() []openroutersdk.Option {
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithOpenRouterAPIMode(openroutersdk.OpenRouterAPIModeResponses))
	return opts
}

func integrationImageModel(t *testing.T) string {
	t.Helper()
	model := os.Getenv("OPENROUTER_IMAGE_MODEL")
	if model == "" {
		t.Skip("OPENROUTER_IMAGE_MODEL is required for image-output integration tests")
	}
	return model
}

func integrationVisionModel(t *testing.T) string {
	t.Helper()
	if model := os.Getenv("OPENROUTER_VISION_MODEL"); model != "" {
		return model
	}
	if model := os.Getenv("OPENROUTER_IMAGE_MODEL"); model != "" {
		return model
	}
	// Default to a known cheap vision-capable model; not all models support image_url blocks.
	return "google/gemini-2.0-flash-lite-001"
}

func collectResult(
	t *testing.T,
	iter func(func(openroutersdk.Message, error) bool),
) *openroutersdk.ResultMessage {
	t.Helper()

	var result *openroutersdk.ResultMessage

	iter(func(msg openroutersdk.Message, err error) bool {
		if err != nil {
			// OpenRouter free-tier routing can land on providers
			// whose limits don't match the requested parameters.
			// Skip rather than fail so flaky routing doesn't break CI.
			if isProviderRoutingError(err) {
				t.Skipf("skipping due to provider routing error: %v", err)
			}

			t.Fatalf("unexpected error: %v", err)
		}

		if rm, ok := msg.(*openroutersdk.ResultMessage); ok {
			result = rm
		}

		return true
	})

	if result == nil {
		t.Fatal("expected result message")
	}

	return result
}

// isProviderRoutingError returns true when the error indicates an
// OpenRouter free-tier provider routing issue (e.g., the selected
// backend doesn't support the requested max_tokens).
func isProviderRoutingError(err error) bool {
	if err == nil {
		return false
	}

	msg := err.Error()

	return strings.Contains(msg, "Provider returned error") ||
		strings.Contains(msg, "maximum allowed is")
}

func waitForSession(
	ctx context.Context,
	sessionID string,
	opts ...openroutersdk.Option,
) (*openroutersdk.SessionStat, error) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		stat, err := openroutersdk.StatSession(ctx, sessionID, opts...)
		if err == nil {
			return stat, nil
		}
		if !errors.Is(err, openroutersdk.ErrSessionNotFound) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func waitForCondition(ctx context.Context, check func() bool) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	for {
		if check() {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
