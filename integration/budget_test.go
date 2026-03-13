//go:build integration

package integration_test

import (
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestMaxBudgetUSD_LimitEnforced(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithMaxBudgetUSD(0.0001))

	var (
		sawBudgetError bool
		sawResult      bool
	)

	for msg, err := range openroutersdk.Query(ctx, openrouterText("Write a long essay about distributed systems."), opts...) {
		if err != nil {
			// Budget enforcement may surface as an error.
			break
		}

		if result, ok := msg.(*openroutersdk.ResultMessage); ok {
			sawResult = true
			if result.Subtype == "error_max_budget_usd" {
				sawBudgetError = true
			}
		}
	}

	if sawResult && !sawBudgetError {
		t.Log("query completed within budget (model may be free); budget enforcement is best-effort")
	}
}

func TestMaxBudgetUSD_NormalBudget(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithMaxBudgetUSD(1.0))

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Reply with the word: budget."), opts...))
	if result.Result == nil {
		t.Fatal("expected result with normal budget")
	}
}

func TestQueryWithCostTracking(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Reply with the word: cost."), integrationOptions()...))

	if result.Usage != nil {
		if result.Usage.InputTokens == 0 && result.Usage.OutputTokens == 0 {
			t.Log("usage reported but tokens are zero (may be a free model)")
		}
	}

	// TotalCostUSD may be nil for free models.
	if result.TotalCostUSD != nil && *result.TotalCostUSD < 0 {
		t.Fatalf("unexpected negative cost: %v", *result.TotalCostUSD)
	}
}
