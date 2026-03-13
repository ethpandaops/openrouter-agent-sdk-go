package exampleutil

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	envAPIKey         = "OPENROUTER_API_KEY"
	envModel          = "OPENROUTER_MODEL"
	envImageModel     = "OPENROUTER_IMAGE_MODEL"
	envVisionModel    = "OPENROUTER_VISION_MODEL"
	defaultModel      = "openrouter/free"
	defaultImageModel = "google/gemini-2.5-flash-image"
)

func APIKey() string {
	return strings.TrimSpace(os.Getenv(envAPIKey))
}

func RequireAPIKey() error {
	if APIKey() == "" {
		return errors.New("missing OPENROUTER_API_KEY")
	}
	return nil
}

func DefaultModel() string {
	if m := strings.TrimSpace(os.Getenv(envModel)); m != "" {
		return m
	}
	return defaultModel
}

func DefaultImageModel() string {
	if m := strings.TrimSpace(os.Getenv(envImageModel)); m != "" {
		return m
	}
	return defaultImageModel
}

func DefaultVisionModel() string {
	if m := strings.TrimSpace(os.Getenv(envVisionModel)); m != "" {
		return m
	}
	if m := strings.TrimSpace(os.Getenv(envModel)); m != "" {
		return m
	}
	return defaultImageModel
}

func PrintMissingAPIKeyHint() {
	fmt.Printf("Set %s to run this example.\\n", envAPIKey)
}
