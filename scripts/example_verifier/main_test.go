package main

import "testing"

func TestResolveVerifyModel_Default(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "")

	if got := resolveVerifyModel(""); got != defaultVerifyModel {
		t.Fatalf("expected default model %q, got %q", defaultVerifyModel, got)
	}
}

func TestResolveVerifyModel_UsesEnvironmentOverride(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "openai/gpt-4o-mini")

	if got := resolveVerifyModel(""); got != "openai/gpt-4o-mini" {
		t.Fatalf("expected env override, got %q", got)
	}
}

func TestResolveVerifyModel_FlagWins(t *testing.T) {
	t.Setenv("OPENROUTER_MODEL", "openrouter/free")

	if got := resolveVerifyModel("google/gemma-3-4b-it:free"); got != "google/gemma-3-4b-it:free" {
		t.Fatalf("expected flag override, got %q", got)
	}
}

func TestParseVerdictText_RawJSON(t *testing.T) {
	got, ok := parseVerdictText(`{"pass":true,"reason":"looks good"}`)
	if !ok {
		t.Fatal("expected raw JSON verdict to parse")
	}
	if !got.Pass || got.Reason != "looks good" {
		t.Fatalf("unexpected verdict: %#v", got)
	}
}

func TestParseVerdictText_FencedJSON(t *testing.T) {
	got, ok := parseVerdictText("```json\n{\"pass\":false,\"reason\":\"expected failure\"}\n```")
	if !ok {
		t.Fatal("expected fenced JSON verdict to parse")
	}
	if got.Pass || got.Reason != "expected failure" {
		t.Fatalf("unexpected verdict: %#v", got)
	}
}

func TestParseVerdictText_JSONEmbeddedInProse(t *testing.T) {
	got, ok := parseVerdictText("Here is the result:\n{\"pass\":true,\"reason\":\"output matches\"}\nThanks.")
	if !ok {
		t.Fatal("expected embedded JSON verdict to parse")
	}
	if !got.Pass || got.Reason != "output matches" {
		t.Fatalf("unexpected verdict: %#v", got)
	}
}
