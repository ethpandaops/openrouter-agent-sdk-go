package openroutersdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/session"
)

func TestStatSession_NotFound(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	store := t.TempDir() + "/sessions.json"
	_, err := StatSession(ctx, "missing", WithSessionStorePath(store))
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestBuildSessionStat_SizeBytes(t *testing.T) {
	s := &session.Session{
		ID:        "test-session",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Messages: []map[string]any{
			{"role": "user", "content": "hello"},
			{"role": "assistant", "content": "world"},
		},
		UserTurns: 1,
	}

	stat := buildSessionStat(s)
	if stat == nil {
		t.Fatal("expected non-nil session stat")
	}
	if stat.SizeBytes <= 0 {
		t.Fatalf("expected SizeBytes > 0, got %d", stat.SizeBytes)
	}
	if stat.MessageCount != 2 {
		t.Fatalf("expected MessageCount=2, got %d", stat.MessageCount)
	}
}

func TestBuildSessionStat_NilSession(t *testing.T) {
	stat := buildSessionStat(nil)
	if stat != nil {
		t.Fatalf("expected nil stat for nil session, got %+v", stat)
	}
}
