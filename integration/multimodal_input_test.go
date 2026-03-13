//go:build integration

package integration_test

import (
	"strings"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

// 8x8 red square PNG — small but large enough for providers to decode.
const tinyPNGDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEklEQVR4nGP4z8CAFWEXHbQSACj/P8Fu7N9hAAAAAElFTkSuQmCC"

func TestMultimodalImageInputQuery(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	model := integrationVisionModel(t)
	content := openroutersdk.Blocks(
		openroutersdk.TextInput("Describe the provided placeholder image in under ten words."),
		openroutersdk.ImageInput(tinyPNGDataURL),
	)

	var result *openroutersdk.ResultMessage
	for msg, err := range openroutersdk.Query(ctx, content,
		openroutersdk.WithModel(model),
		openroutersdk.WithMaxTurns(2),
		openroutersdk.WithTemperature(0),
	) {
		if err != nil {
			// Vision support is provider-dependent; skip on provider errors.
			t.Skipf("provider error (model %s may not support image input): %v", model, err)
		}
		if r, ok := msg.(*openroutersdk.ResultMessage); ok {
			result = r
		}
	}
	if result == nil || result.Result == nil {
		t.Fatal("expected result from multimodal input query")
	}
	if strings.TrimSpace(*result.Result) == "" {
		t.Fatalf("expected non-empty multimodal result, got %+v", result)
	}
}
