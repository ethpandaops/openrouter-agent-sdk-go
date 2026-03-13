//go:build integration

package integration_test

import (
	"os"
	"path/filepath"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestImageGenerationProducesAssistantImageBlock(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	model := integrationImageModel(t)
	opts := []openroutersdk.Option{
		openroutersdk.WithModel(model),
		openroutersdk.WithTemperature(0),
		openroutersdk.WithMaxTurns(4),
		openroutersdk.WithModalities("text", "image"),
		openroutersdk.WithImageConfig(map[string]any{"aspect_ratio": "1:1"}),
		openroutersdk.WithMaxTokens(220),
	}

	var (
		result   *openroutersdk.ResultMessage
		imageOut *openroutersdk.ImageBlock
	)
	for msg, err := range openroutersdk.Query(ctx, openrouterText("Design a minimal app icon and return the generated image with a one-line description."), opts...) {
		if err != nil {
			t.Fatalf("query error: %v", err)
		}
		switch m := msg.(type) {
		case *openroutersdk.AssistantMessage:
			for _, block := range m.Content {
				image, ok := block.(*openroutersdk.ImageBlock)
				if ok {
					imageOut = image
				}
			}
		case *openroutersdk.ResultMessage:
			result = m
		}
	}

	if imageOut == nil {
		t.Fatalf("expected generated image block, got result=%+v", result)
	}
	data, mediaType, err := imageOut.Decode()
	if err != nil {
		t.Fatalf("decode generated image: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty generated image data")
	}
	if mediaType == "" {
		t.Fatal("expected generated image media type")
	}

	path := filepath.Join(t.TempDir(), "generated"+imageOut.FileExtension())
	if err := imageOut.Save(path); err != nil {
		t.Fatalf("save generated image: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat saved image: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected saved image file to be non-empty")
	}
}
