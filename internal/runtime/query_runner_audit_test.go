package runtime

import (
	"encoding/json"
	"testing"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAttachAuditEnvelope_AssistantMessage(t *testing.T) {
	am := &message.AssistantMessage{
		Type:  "assistant",
		Model: "test-model",
		Content: []message.ContentBlock{
			&message.TextBlock{Type: "text", Text: "hello"},
		},
	}

	attachAuditEnvelope(am, "assistant", "final_text", am)

	require.NotNil(t, am.Audit)
	assert.Equal(t, "assistant", am.Audit.EventType)
	assert.Equal(t, "final_text", am.Audit.Subtype)
	assert.NotNil(t, am.Audit.Payload)
}

func TestAttachAuditEnvelope_ResultMessage(t *testing.T) {
	result := "done"
	rm := &message.ResultMessage{
		Type:      "result",
		Subtype:   "success",
		SessionID: "sess-1",
		Result:    &result,
	}

	attachAuditEnvelope(rm, "result", rm.Subtype, rm)

	require.NotNil(t, rm.Audit)
	assert.Equal(t, "result", rm.Audit.EventType)
	assert.Equal(t, "success", rm.Audit.Subtype)
}

func TestAttachAuditEnvelope_SystemMessage(t *testing.T) {
	sm := &message.SystemMessage{
		Type:    "system",
		Subtype: "init",
	}

	attachAuditEnvelope(sm, "system", "init", sm)

	require.NotNil(t, sm.Audit)
	assert.Equal(t, "system", sm.Audit.EventType)
	assert.Equal(t, "init", sm.Audit.Subtype)
}

func TestAttachAuditEnvelope_StreamEvent(t *testing.T) {
	se := &message.StreamEvent{
		Event: map[string]any{"type": "content_block_delta"},
	}

	attachAuditEnvelope(se, "stream_event", "", se)

	require.NotNil(t, se.Audit)
	assert.Equal(t, "stream_event", se.Audit.EventType)
}

func TestAttachAuditEnvelope_UserMessage(t *testing.T) {
	um := &message.UserMessage{
		Type: "user",
	}

	attachAuditEnvelope(um, "user", "", um)

	require.NotNil(t, um.Audit)
	assert.Equal(t, "user", um.Audit.EventType)
}

func TestAttachAuditEnvelope_MarshalError_Nops(t *testing.T) {
	am := &message.AssistantMessage{Type: "assistant"}

	// Channels can't be marshaled — should silently nop.
	attachAuditEnvelope(am, "assistant", "final_text", make(chan int))

	assert.Nil(t, am.Audit)
}

func TestNewAuditEnvelope_RoundTrip(t *testing.T) {
	type payload struct {
		Key string `json:"key"`
	}

	env, err := message.NewAuditEnvelope("test_event", "test_sub", payload{Key: "val"})
	require.NoError(t, err)
	require.NotNil(t, env)
	assert.Equal(t, "test_event", env.EventType)
	assert.Equal(t, "test_sub", env.Subtype)

	var p map[string]any
	err = json.Unmarshal(env.Payload, &p)
	require.NoError(t, err)
	assert.Equal(t, "val", p["key"])
}

func TestNewAuditEnvelope_MarshalError(t *testing.T) {
	env, err := message.NewAuditEnvelope("event", "sub", make(chan int))
	assert.Error(t, err)
	assert.Nil(t, env)
}
