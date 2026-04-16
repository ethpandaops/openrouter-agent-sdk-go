package openrouter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ethpandaops/agent-sdk-observability/testkit"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/observability"
)

func TestDoRequestObservabilityRecordsRetriesAndRateLimit(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			http.Error(w, "slow down", http.StatusTooManyRequests)
		default:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {}\n\n"))
		}
	}))
	defer server.Close()

	metrics := testkit.NewMetricsHarness()
	defer func() { _ = metrics.Shutdown(context.Background()) }()
	traces := testkit.NewTracesHarness()
	defer func() { _ = traces.Shutdown(context.Background()) }()

	tr := NewHTTPTransport(&config.Options{})
	tr.baseURL = server.URL
	tr.apiKey = "test-key"
	tr.started = true
	tr.client = server.Client()
	obs, err := observability.New(observability.Config{
		MeterProvider:  metrics.Provider(),
		TracerProvider: traces.Provider(),
	})
	if err != nil {
		t.Fatalf("observability.New() error = %v", err)
	}
	tr.SetObserver(obs)

	resp, err := tr.doRequest(context.Background(), []byte(`{"stream":true}`))
	if err != nil {
		t.Fatalf("doRequest() error = %v", err)
	}
	_ = resp.Body.Close()

	httpPoints, err := metrics.Int64Points(context.Background(), "openrouter.http_requests_total")
	if err != nil {
		t.Fatalf("Int64Points(http_requests_total) error = %v", err)
	}
	if len(httpPoints) != 2 {
		t.Fatalf("expected 2 http request points, got %d", len(httpPoints))
	}

	httpDurationPoints, err := metrics.HistogramPoints(context.Background(), "openrouter.http_request_duration")
	if err != nil {
		t.Fatalf("HistogramPoints(http_request_duration) error = %v", err)
	}
	if len(httpDurationPoints) != 2 {
		t.Fatalf("expected 2 http duration points, got %d", len(httpDurationPoints))
	}

	rateLimitPoints, err := metrics.Int64Points(context.Background(), "openrouter.rate_limit_events_total")
	if err != nil {
		t.Fatalf("Int64Points(rate_limit_events_total) error = %v", err)
	}
	if len(rateLimitPoints) != 1 {
		t.Fatalf("expected 1 rate-limit point, got %d", len(rateLimitPoints))
	}

	var sawHTTPSpan bool
	var sawRetryEvent bool
	var sawRateLimitEvent bool
	for _, span := range traces.Summaries() {
		if span.Name != "openrouter.http.request" {
			continue
		}
		sawHTTPSpan = true
		for _, ev := range span.Events {
			if ev == "retry" {
				sawRetryEvent = true
			}
			if ev == "rate_limit" {
				sawRateLimitEvent = true
			}
		}
	}
	if !sawHTTPSpan || !sawRetryEvent || !sawRateLimitEvent {
		t.Fatalf("expected http span with retry/rate_limit events, got span=%v retry=%v rate_limit=%v", sawHTTPSpan, sawRetryEvent, sawRateLimitEvent)
	}
}
