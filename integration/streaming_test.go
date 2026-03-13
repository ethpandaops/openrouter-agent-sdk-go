//go:build integration

package integration_test

import (
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestQueryStream(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	stream := openroutersdk.MessagesFromContent(openrouterText("Reply with the single word: streamed."))
	for msg, err := range openroutersdk.QueryStream(ctx, stream, integrationOptions()...) {
		if err != nil {
			t.Fatalf("query stream error: %v", err)
		}
		if _, ok := msg.(*openroutersdk.ResultMessage); ok {
			return
		}
	}

	t.Fatal("expected result message")
}
