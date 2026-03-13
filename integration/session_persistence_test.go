//go:build integration

package integration_test

import (
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestSessionPersistence(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	store := t.TempDir() + "/sessions.json"
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithSessionStorePath(store))

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Reply with the word persisted."), opts...))
	if result.SessionID == "" {
		t.Fatal("expected session id")
	}

	stat, err := waitForSession(ctx, result.SessionID, openroutersdk.WithSessionStorePath(store))
	if err != nil {
		t.Fatalf("wait for session: %v", err)
	}
	if stat.MessageCount == 0 {
		t.Fatalf("expected persisted messages: %+v", stat)
	}

	sessions, err := openroutersdk.ListSessions(ctx, openroutersdk.WithSessionStorePath(store))
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected persisted sessions")
	}

	msgs, err := openroutersdk.GetSessionMessages(ctx, result.SessionID, openroutersdk.WithSessionStorePath(store))
	if err != nil {
		t.Fatalf("get session messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected persisted session messages")
	}
}
