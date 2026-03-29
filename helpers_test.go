package openroutersdk

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

type scriptedTransport struct {
	t       *testing.T
	mu      sync.Mutex
	call    int
	scripts []func(*ChatRequest) ([]map[string]any, error)
}

func (t *scriptedTransport) Start(context.Context) error { return nil }
func (t *scriptedTransport) Close() error                { return nil }

func (t *scriptedTransport) CreateStream(
	_ context.Context,
	req *ChatRequest,
) (<-chan map[string]any, <-chan error) {
	t.mu.Lock()
	index := t.call
	t.call++
	t.mu.Unlock()

	out := make(chan map[string]any, 8)
	errs := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errs)

		if index >= len(t.scripts) {
			t.t.Errorf("unexpected transport call %d", index+1)
			return
		}
		events, err := t.scripts[index](req)
		if err != nil {
			errs <- err
			return
		}
		for _, event := range events {
			out <- event
		}
	}()

	return out, errs
}

func writeModelResponse(t *testing.T, w http.ResponseWriter, data []map[string]any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		t.Fatalf("encode model response: %v", err)
	}
}
