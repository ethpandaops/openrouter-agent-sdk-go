package openroutersdk

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

// TestReceiveResponse_DrainsMessagesAfterResult is a regression test
// proving that messages arriving AFTER a ResultMessage are silently
// drained rather than blocking the producer goroutine. Without the
// drain-after-result fix, the producer would be blocked forever
// trying to send on a full channel (goroutine leak).
func TestReceiveResponse_DrainsMessagesAfterResult(t *testing.T) {
	// Use buffered channels to simulate a producer that sends
	// additional messages after the result.
	msgs := make(chan message.Message, 5)
	errs := make(chan error, 1)

	c := &clientImpl{
		connected:   true,
		currentMsgs: msgs,
		currentErrs: errs,
	}

	// Producer sends: text message, result, then 3 more messages.
	msgs <- &message.AssistantMessage{
		Content: []message.ContentBlock{
			&message.TextBlock{Text: "thinking..."},
		},
	}
	msgs <- &message.ResultMessage{
		Type:      "result",
		Subtype:   "success",
		SessionID: "default",
	}
	msgs <- &message.AssistantMessage{
		Content: []message.ContentBlock{
			&message.TextBlock{Text: "post-result-1"},
		},
	}
	msgs <- &message.AssistantMessage{
		Content: []message.ContentBlock{
			&message.TextBlock{Text: "post-result-2"},
		},
	}
	msgs <- &message.AssistantMessage{
		Content: []message.ContentBlock{
			&message.TextBlock{Text: "post-result-3"},
		},
	}
	close(msgs)
	close(errs)

	// Consumer reads all yielded messages.
	var received []message.Message

	for msg, err := range c.ReceiveResponse(context.Background()) {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		received = append(received, msg)
	}

	// Consumer should only see messages up to and including the
	// ResultMessage. Post-result messages should be drained silently.
	require.Len(t, received, 2,
		"consumer should receive exactly 2 messages: "+
			"assistant text + result; post-result messages "+
			"should be drained silently")

	_, isResult := received[1].(*message.ResultMessage)
	assert.True(t, isResult,
		"second yielded message should be the ResultMessage")
}

// TestReceiveResponse_DrainsErrorsAfterResult proves that errors
// arriving after the ResultMessage is consumed are drained without
// being yielded to the consumer.
func TestReceiveResponse_DrainsErrorsAfterResult(t *testing.T) {
	// Use unbuffered msgs so we control the ordering precisely.
	msgs := make(chan message.Message)
	errs := make(chan error, 2)

	c := &clientImpl{
		connected:   true,
		currentMsgs: msgs,
		currentErrs: errs,
	}

	done := make(chan struct{})

	var receivedMsgs []message.Message

	var receivedErrs []error

	go func() {
		defer close(done)

		for msg, err := range c.ReceiveResponse(
			context.Background(),
		) {
			if err != nil {
				receivedErrs = append(receivedErrs, err)
			}
			if msg != nil {
				receivedMsgs = append(receivedMsgs, msg)
			}
		}
	}()

	// Send result on the msgs channel — blocks until consumer reads it.
	msgs <- &message.ResultMessage{
		Type:      "result",
		Subtype:   "success",
		SessionID: "default",
	}

	// Now that the result has been consumed (the send above unblocked),
	// inject errors. These should be drained, not yielded.
	errs <- errors.New("post-result transport error")
	errs <- errors.New("another post-result error")
	close(errs)
	close(msgs)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ReceiveResponse did not finish")
	}

	require.Len(t, receivedMsgs, 1,
		"should receive exactly the ResultMessage")
	assert.Empty(t, receivedErrs,
		"errors after result should be drained, not yielded")
}

// TestReceiveResponse_ContextCancelStopsIteration proves that
// cancelling the context causes ReceiveResponse to stop and
// yield a context error, even while draining.
func TestReceiveResponse_ContextCancelStopsIteration(
	t *testing.T,
) {
	msgs := make(chan message.Message)
	errs := make(chan error)

	c := &clientImpl{
		connected:   true,
		currentMsgs: msgs,
		currentErrs: errs,
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})

	go func() {
		defer close(done)

		for _, err := range c.ReceiveResponse(ctx) {
			if err != nil {
				return
			}
		}
	}()

	// Cancel before sending anything.
	cancel()

	select {
	case <-done:
		// Good — iterator exited due to context cancel.
	case <-time.After(2 * time.Second):
		t.Fatal(
			"ReceiveResponse should exit promptly on context cancel",
		)
	}
}
