package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/config"
	internalerrors "github.com/ethpandaops/openrouter-agent-sdk-go/internal/errors"
)

// TestCreateStream_SlowButProgressing ensures the default short request
// timeout does not kill a stream that is slowly but steadily emitting data.
// This is a regression guard for issue #9: http.Client.Timeout used to cap
// the full body-read window.
func TestCreateStream_SlowButProgressing(t *testing.T) {
	const chunkDelay = 40 * time.Millisecond
	const chunks = 5

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		for i := range chunks {
			time.Sleep(chunkDelay)
			_, _ = fmt.Fprintf(w, "data: {\"i\":%d}\n\n", i)
			if flusher != nil {
				flusher.Flush()
			}
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	// Overall budget shorter than the old Client.Timeout-style total body
	// duration (5 * 40ms = 200ms) would have been rejected; idle budget is
	// long enough that per-chunk progress keeps it alive.
	idle := 150 * time.Millisecond
	opts := &config.Options{
		BaseURL:           srv.URL,
		APIKey:            "sk-test",
		StreamIdleTimeout: &idle,
	}
	tr := NewHTTPTransport(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, errs := tr.CreateStream(ctx, &config.ChatRequest{
		Model:    "test",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})

	got := 0
	for msg := range out {
		if _, ok := msg["i"]; ok {
			got++
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("unexpected stream error: %v", err)
		}
	}
	if got != chunks {
		t.Fatalf("expected %d data chunks, got %d", chunks, got)
	}
}

// TestCreateStream_IdleStall ensures a stalled stream is aborted with
// ErrStreamIdle when no data arrives within StreamIdleTimeout.
func TestCreateStream_IdleStall(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Never emit data. Unblocks only when the test tears down.
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer func() {
		close(block)
		srv.Close()
	}()

	idle := 80 * time.Millisecond
	opts := &config.Options{
		BaseURL:           srv.URL,
		APIKey:            "sk-test",
		StreamIdleTimeout: &idle,
	}
	tr := NewHTTPTransport(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out, errs := tr.CreateStream(ctx, &config.ChatRequest{
		Model:    "test",
		Messages: []map[string]any{{"role": "user", "content": "hi"}},
	})

	for range out {
		// drain
	}

	var seen error
	for err := range errs {
		if err != nil {
			seen = err
		}
	}
	if seen == nil {
		t.Fatalf("expected stream error, got nil")
	}
	if !errors.Is(seen, internalerrors.ErrStreamIdle) {
		t.Fatalf("expected ErrStreamIdle, got %v", seen)
	}
}

// TestNewHTTPTransport_NoClientTimeout documents the invariant: the streaming
// client must never have http.Client.Timeout set, since that caps body read.
func TestNewHTTPTransport_NoClientTimeout(t *testing.T) {
	timeout := 5 * time.Second
	tr := NewHTTPTransport(&config.Options{RequestTimeout: &timeout})
	if tr.client.Timeout != 0 {
		t.Fatalf("expected client.Timeout=0, got %s", tr.client.Timeout)
	}
	httpTransport, ok := tr.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport, got %T", tr.client.Transport)
	}
	if httpTransport.ResponseHeaderTimeout == 0 {
		t.Fatalf("expected ResponseHeaderTimeout to be set")
	}
}
