package main

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/ethpandaops/openrouter-agent-sdk-go"
	"github.com/ethpandaops/openrouter-agent-sdk-go/examples/internal/exampleutil"
)

// 8x8 red square PNG — small but large enough for providers to decode.
const sampleImageDataURL = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAIAAABLbSncAAAAEklEQVR4nGP4z8CAFWEXHbQSACj/P8Fu7N9hAAAAAElFTkSuQmCC"

func main() {
	if err := exampleutil.RequireAPIKey(); err != nil {
		exampleutil.PrintMissingAPIKeyHint()
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	content := sdk.Blocks(
		sdk.TextInput("Describe the two attached sample images in one short sentence and mention that they are identical placeholders."),
		sdk.ImageInput(sampleImageDataURL),
		sdk.ImageInput(sampleImageDataURL),
	)

	fmt.Println("Using vision model:", exampleutil.DefaultVisionModel())
	fmt.Println("Set OPENROUTER_VISION_MODEL to override for a different multimodal input model.")

	for msg, err := range sdk.Query(ctx, content,
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultVisionModel()),
		sdk.WithMaxTurns(2),
		sdk.WithTemperature(0),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}
}
