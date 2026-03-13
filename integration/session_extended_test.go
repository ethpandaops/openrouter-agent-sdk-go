//go:build integration

package integration_test

import (
	"testing"

	openroutersdk "github.com/ethpandaops/openrouter-agent-sdk-go"
)

func TestSessionPersistence_ResumeSession(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	store := t.TempDir() + "/sessions.json"
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithSessionStorePath(store))

	// First query — creates session.
	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Remember the word: elephant. Reply with just 'remembered'."), opts...))
	if result.SessionID == "" {
		t.Fatal("expected session id")
	}

	sessionID := result.SessionID

	// Wait for persistence.
	if _, err := waitForSession(ctx, sessionID, openroutersdk.WithSessionStorePath(store)); err != nil {
		t.Fatalf("wait for session: %v", err)
	}

	// Resume session with second query.
	resumeOpts := append([]openroutersdk.Option{}, integrationOptions()...)
	resumeOpts = append(resumeOpts,
		openroutersdk.WithSessionStorePath(store),
		openroutersdk.WithResume(sessionID),
	)

	result2 := collectResult(t, openroutersdk.Query(ctx, openrouterText("What word did I ask you to remember?"), resumeOpts...))
	if result2.Result == nil {
		t.Fatal("expected result from resumed session")
	}
}

func TestSessionPersistence_MessageRetrieval(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	store := t.TempDir() + "/sessions.json"
	opts := append([]openroutersdk.Option{}, integrationOptions()...)
	opts = append(opts, openroutersdk.WithSessionStorePath(store))

	result := collectResult(t, openroutersdk.Query(ctx, openrouterText("Reply with exactly: test-message."), opts...))
	if result.SessionID == "" {
		t.Fatal("expected session id")
	}

	// Wait for persistence.
	stat, err := waitForSession(ctx, result.SessionID, openroutersdk.WithSessionStorePath(store))
	if err != nil {
		t.Fatalf("wait for session: %v", err)
	}

	// Verify message count.
	if stat.MessageCount == 0 {
		t.Fatalf("expected messages in session, got %+v", stat)
	}
	if stat.UserTurns == 0 {
		t.Fatalf("expected user turns > 0, got %+v", stat)
	}

	// Verify message retrieval.
	msgs, err := openroutersdk.GetSessionMessages(ctx, result.SessionID, openroutersdk.WithSessionStorePath(store))
	if err != nil {
		t.Fatalf("get session messages: %v", err)
	}
	if len(msgs) == 0 {
		t.Fatal("expected persisted messages")
	}

	// Verify we have both user and assistant messages.
	var sawUser, sawAssistant bool
	for _, msg := range msgs {
		switch msg.(type) {
		case *openroutersdk.UserMessage:
			sawUser = true
		case *openroutersdk.AssistantMessage:
			sawAssistant = true
		}
	}

	if !sawUser {
		t.Fatal("expected user message in session")
	}
	if !sawAssistant {
		t.Fatal("expected assistant message in session")
	}
}

func TestSessionNotFound(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	store := t.TempDir() + "/sessions.json"
	_, err := openroutersdk.StatSession(ctx, "nonexistent-session-id", openroutersdk.WithSessionStorePath(store))

	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
}
