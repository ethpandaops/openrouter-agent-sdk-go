// Example prometheus_metrics demonstrates how to expose SDK metrics
// via a Prometheus /metrics endpoint using WithPrometheusRegisterer.
package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	sdk "github.com/ethpandaops/openrouter-agent-sdk-go"
	"github.com/ethpandaops/openrouter-agent-sdk-go/examples/internal/exampleutil"
)

func main() {
	if err := exampleutil.RequireAPIKey(); err != nil {
		exampleutil.PrintMissingAPIKeyHint()
		return
	}

	// Create a Prometheus registry and wire it into the SDK.
	reg := prometheus.NewRegistry()

	// Start a metrics HTTP server.
	go func() {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		fmt.Println("Prometheus metrics available at http://localhost:9090/metrics")

		server := &http.Server{
			Addr:              ":9090",
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Printf("metrics server error: %v\n", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	// Run a query with Prometheus metrics enabled.
	for msg, err := range sdk.Query(ctx, sdk.Text("What is 2+2? Answer in one word."),
		sdk.WithAPIKey(exampleutil.APIKey()),
		sdk.WithModel(exampleutil.DefaultModel()),
		sdk.WithMaxTurns(1),
		sdk.WithPrometheusRegisterer(reg),
	) {
		if err != nil {
			fmt.Printf("query error: %v\n", err)
			return
		}
		exampleutil.DisplayMessage(msg)
	}

	fmt.Println("\nQuery complete. Metrics are available at http://localhost:9090/metrics")
	fmt.Println("Press Ctrl+C to exit.")

	// Keep the process alive so you can inspect /metrics.
	select {}
}
