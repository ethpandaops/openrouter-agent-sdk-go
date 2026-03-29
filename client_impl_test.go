package openroutersdk

import (
	"context"
	"testing"
	"time"

	"github.com/ethpandaops/openrouter-agent-sdk-go/internal/message"
)

func TestReceiveResponse_WaitsForProducerShutdownAfterResult(
	t *testing.T,
) {
	msgs := make(chan message.Message, 1)
	errs := make(chan error)

	c := &clientImpl{
		connected:   true,
		currentMsgs: msgs,
		currentErrs: errs,
	}

	done := make(chan struct{})

	go func() {
		defer close(done)
		for range c.ReceiveResponse(context.Background()) {
		}
	}()

	msgs <- &message.ResultMessage{
		Type:      "result",
		Subtype:   "success",
		SessionID: "default",
	}

	select {
	case <-done:
		t.Fatal("ReceiveResponse returned before producer shutdown; post-result cleanup can be skipped")
	case <-time.After(50 * time.Millisecond):
	}

	close(msgs)
	close(errs)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ReceiveResponse did not finish after producer shutdown")
	}
}
