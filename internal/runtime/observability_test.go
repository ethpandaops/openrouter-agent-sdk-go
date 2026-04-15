package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ethpandaops/agent-sdk-observability/testkit"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/observability"
	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
)

type eventTransport struct {
	events []map[string]any
}

func (t *eventTransport) Start(context.Context) error { return nil }
func (t *eventTransport) Close() error                { return nil }
func (t *eventTransport) CreateStream(
	_ context.Context,
	_ *config.ChatRequest,
) (<-chan map[string]any, <-chan error) {
	out := make(chan map[string]any, len(t.events))
	errs := make(chan error)
	for _, ev := range t.events {
		out <- ev
	}
	close(out)
	close(errs)
	return out, errs
}

func TestRunPromptObservabilityRecordsQueryMetricsAndSpan(t *testing.T) {
	metrics := testkit.NewMetricsHarness()
	defer func() { _ = metrics.Shutdown(context.Background()) }()
	traces := testkit.NewTracesHarness()
	defer func() { _ = traces.Shutdown(context.Background()) }()

	opts := &config.Options{
		Transport:      &captureTransport{content: "hello"},
		MaxTurns:       1,
		MeterProvider:  metrics.Provider(),
		TracerProvider: traces.Provider(),
	}
	obs, err := observability.New(observability.Config{
		MeterProvider:  metrics.Provider(),
		TracerProvider: traces.Provider(),
	})
	if err != nil {
		t.Fatalf("observability.New() error = %v", err)
	}
	r := NewQueryRunner(opts, session.NewManager(), obs)

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	ttftPoints, err := metrics.HistogramPoints(context.Background(), "gen_ai.client.operation.time_to_first_chunk")
	if err != nil {
		t.Fatalf("HistogramPoints(ttft) error = %v", err)
	}
	if len(ttftPoints) == 0 {
		t.Fatal("expected TTFT metric point")
	}

	opPoints, err := metrics.HistogramPoints(context.Background(), "gen_ai.client.operation.duration")
	if err != nil {
		t.Fatalf("HistogramPoints(operation) error = %v", err)
	}
	if len(opPoints) == 0 {
		t.Fatal("expected operation duration metric point")
	}

	var sawQuerySpan bool
	for _, span := range traces.Summaries() {
		if strings.HasPrefix(span.Name, "chat ") {
			sawQuerySpan = true
			break
		}
	}
	if !sawQuerySpan {
		t.Fatal("expected query span with name \"chat {model}\"")
	}
}

func TestRunPromptObservabilitySkipsTTFTForMetadataOnlyStream(t *testing.T) {
	metrics := testkit.NewMetricsHarness()
	defer func() { _ = metrics.Shutdown(context.Background()) }()

	opts := &config.Options{
		Transport: &eventTransport{
			events: []map[string]any{
				{"type": "response.created"},
				{"type": "response.completed"},
			},
		},
		MaxTurns:      1,
		MeterProvider: metrics.Provider(),
	}
	obs, err := observability.New(observability.Config{MeterProvider: metrics.Provider()})
	if err != nil {
		t.Fatalf("observability.New() error = %v", err)
	}
	r := NewQueryRunner(opts, session.NewManager(), obs)

	msgs, errs := r.RunPrompt(context.Background(), "default", message.NewUserMessageContent("hello"))
	for range msgs {
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	ttftPoints, err := metrics.HistogramPoints(context.Background(), "gen_ai.client.operation.time_to_first_chunk")
	if err != nil {
		t.Fatalf("HistogramPoints(ttft) error = %v", err)
	}
	if len(ttftPoints) != 0 {
		t.Fatalf("expected no TTFT metric for metadata-only stream, got %d points", len(ttftPoints))
	}
}
