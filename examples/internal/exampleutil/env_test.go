package exampleutil

import "testing"

func TestDefaultModelFallback(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "")
	got := DefaultModel()
	if got != "openrouter/free" {
		t.Fatalf("unexpected default model: %q", got)
	}
}

func TestDefaultModelOverride(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "google/gemma-3-4b-it:free")
	got := DefaultModel()
	if got != "google/gemma-3-4b-it:free" {
		t.Fatalf("unexpected override model: %q", got)
	}
}

func TestDefaultImageModelFallback(t *testing.T) {
	t.Setenv("OPENROUTER_IMAGE_MODEL", "")
	got := DefaultImageModel()
	if got != "google/gemini-2.5-flash-image" {
		t.Fatalf("unexpected default image model: %q", got)
	}
}

func TestDefaultImageModelOverride(t *testing.T) {
	t.Setenv("OPENROUTER_IMAGE_MODEL", "meta-llama/llama-3.2-11b-vision-instruct")
	got := DefaultImageModel()
	if got != "meta-llama/llama-3.2-11b-vision-instruct" {
		t.Fatalf("unexpected override image model: %q", got)
	}
}
