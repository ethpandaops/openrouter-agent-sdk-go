//go:build integration

package integration_test

import (
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestQuery(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	var sawResult bool
	for msg, err := range openroutersdk.Query(ctx, openrouterText("Reply with the single word: pong."), integrationOptions()...) {
		if err != nil {
			t.Fatalf("query error: %v", err)
		}
		if result, ok := msg.(*openroutersdk.ResultMessage); ok && result.Result != nil {
			sawResult = true
		}
	}

	if !sawResult {
		t.Fatal("expected result message")
	}
}
