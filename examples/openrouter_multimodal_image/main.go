package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
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

	outputDir, err := imageOutputDir()
	if err != nil {
		fmt.Printf("setup error: %v\n", err)
		return
	}

	fmt.Println("Using image model:", exampleutil.DefaultImageModel())
	fmt.Println("Saving generated images under:", outputDir)
	fmt.Println("Set OPENROUTER_IMAGE_MODEL to override for a different image-capable provider/model.")

	if err := runImageCapableQuery(ctx, outputDir); err != nil {
		fmt.Printf("query error: %v\n", err)
	}
}

func imageOutputDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("OPENROUTER_IMAGE_OUTPUT_DIR")); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", err
		}
		return dir, nil
	}
	return os.MkdirTemp("", "openrouter-generated-images-*")
}

func runImageCapableQuery(ctx context.Context, outputDir string) error {
	savedCount := 0
	for msg, err := range sdk.Query(ctx, sdk.Text("Design a simple app icon concept and describe the visual style."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultImageModel()),
		sdk.WithModalities("text", "image"),
		sdk.WithImageConfig(map[string]any{"aspect_ratio": "1:1"}),
		sdk.WithMaxTokens(220),
	) {
		if err != nil {
			if strings.Contains(err.Error(), "requested output modalities") {
				fmt.Println("Image output unavailable on this model/router; retrying with text-only fallback.")
				return runTextOnlyFallback(ctx)
			}
			if strings.Contains(err.Error(), "status=429") {
				fmt.Println("Provider rate-limited this model; treat as expected transient limitation for the free fallback.")
				return nil
			}
			return err
		}
		if assistant, ok := msg.(*sdk.AssistantMessage); ok {
			for _, block := range assistant.Content {
				image, ok := block.(*sdk.ImageBlock)
				if !ok {
					continue
				}
				savedCount++
				path := filepath.Join(outputDir, fmt.Sprintf("generated-%02d%s", savedCount, image.FileExtension()))
				if err := image.Save(path); err != nil {
					return fmt.Errorf("save generated image: %w", err)
				}
				fmt.Printf("Saved generated image: %s\n", path)
			}
		}
		exampleutil.DisplayMessage(msg)
	}
	if savedCount == 0 {
		fmt.Println("No generated images were returned; retrying with text-only fallback.")
		return runTextOnlyFallback(ctx)
	}
	return nil
}

func runTextOnlyFallback(ctx context.Context) error {
	for msg, err := range sdk.Query(ctx, sdk.Text("Return a text-only design brief for a simple app icon concept and describe the visual style."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultImageModel()),
		sdk.WithMaxTokens(220),
	) {
		if err != nil {
			if strings.Contains(err.Error(), "status=429") {
				fmt.Println("Text-only fallback was rate-limited; treat as expected transient limitation for the free fallback.")
				return nil
			}
			return err
		}
		exampleutil.DisplayMessage(msg)
	}
	return nil
}
