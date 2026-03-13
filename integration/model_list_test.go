//go:build integration

package integration_test

import (
	"strings"
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestModelDiscovery(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	resp, err := openroutersdk.ListModelsResponse(ctx, integrationOptions()...)
	if err != nil {
		t.Fatalf("list models response: %v", err)
	}
	if len(resp.Models) == 0 {
		t.Fatal("expected models")
	}
	if resp.Source != "openrouter" {
		t.Fatalf("unexpected source: %+v", resp)
	}
	if !resp.Authenticated {
		t.Fatalf("expected authenticated discovery, got %+v", resp)
	}
	if resp.Endpoint != "models/user" {
		t.Fatalf("expected models/user endpoint, got %+v", resp)
	}
	if resp.Total != len(resp.Models) {
		t.Fatalf("expected total=%d got %d", len(resp.Models), resp.Total)
	}

	var sawFree bool
	var sawToolCapable bool
	var sawStructured bool
	for _, m := range resp.Models {
		if strings.TrimSpace(m.ID) == "" {
			t.Fatalf("model missing id: %#v", m)
		}
		if m.MaxContextLength() <= 0 {
			t.Fatalf("model missing context length: %#v", m)
		}
		if m.CostTier() == "free" {
			sawFree = true
		}
		if m.SupportsToolCalling() {
			sawToolCapable = true
		}
		if m.SupportsStructuredOutput() {
			sawStructured = true
		}
	}
	if !sawFree {
		t.Fatal("expected at least one free-capable model in authenticated discovery")
	}
	if !sawToolCapable {
		t.Fatal("expected at least one tool-capable model in authenticated discovery")
	}
	if !sawStructured {
		t.Fatal("expected at least one structured-output-capable model in authenticated discovery")
	}
}
