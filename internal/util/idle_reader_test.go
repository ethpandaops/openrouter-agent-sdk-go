package util

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// slowReader emits configured chunks with a delay in between.
type slowReader struct {
	chunks []string
	delay  time.Duration
	idx    int
}

func (s *slowReader) Read(p []byte) (int, error) {
	if s.idx >= len(s.chunks) {
		return 0, io.EOF
	}
	if s.idx > 0 {
		time.Sleep(s.delay)
	}
	n := copy(p, s.chunks[s.idx])
	s.idx++
	return n, nil
}

// stuckReader blocks indefinitely on Read until closed via ctx/timer expiry.
type stuckReader struct{ done chan struct{} }

func (s *stuckReader) Read(p []byte) (int, error) {
	<-s.done
	return 0, io.EOF
}

var errIdleSentinel = errors.New("idle sentinel")

func TestIdleReader_ResetsOnProgress(t *testing.T) {
	// Four 20ms chunks with 80ms idle timeout must succeed because each
	// Read resets the timer.
	r := &slowReader{
		chunks: []string{"a", "b", "c", "d"},
		delay:  20 * time.Millisecond,
	}
	fired := make(chan struct{}, 1)
	ir := NewIdleReader(r, 80*time.Millisecond, errIdleSentinel, func() {
		fired <- struct{}{}
	})
	defer ir.Stop()

	buf := make([]byte, 1)
	var total int
	for {
		n, err := ir.Read(buf)
		total += n
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
	}
	if total != 4 {
		t.Fatalf("expected 4 bytes, got %d", total)
	}
	select {
	case <-fired:
		t.Fatalf("idle fired on progressing stream")
	default:
	}
}

func TestIdleReader_FiresOnStall(t *testing.T) {
	stuck := &stuckReader{done: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(stuck.done) }) }
	t.Cleanup(unblock)

	fired := make(chan struct{}, 1)
	ir := NewIdleReader(stuck, 30*time.Millisecond, errIdleSentinel, func() {
		unblock()
		fired <- struct{}{}
	})
	defer ir.Stop()

	buf := make([]byte, 1)
	_, err := ir.Read(buf)
	if !errors.Is(err, errIdleSentinel) {
		t.Fatalf("expected idle sentinel, got %v", err)
	}

	select {
	case <-fired:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("onIdle callback never fired")
	}
}

func TestIdleReader_StopPreventsFire(t *testing.T) {
	stuck := &stuckReader{done: make(chan struct{})}
	t.Cleanup(func() { close(stuck.done) })

	fired := make(chan struct{}, 1)
	ir := NewIdleReader(stuck, 20*time.Millisecond, errIdleSentinel, func() {
		fired <- struct{}{}
	})
	ir.Stop()

	time.Sleep(60 * time.Millisecond)
	select {
	case <-fired:
		t.Fatalf("onIdle fired after Stop")
	default:
	}
}

func TestIdleReader_ZeroTimeoutDisables(t *testing.T) {
	r := &slowReader{chunks: []string{"x"}}
	ir := NewIdleReader(r, 0, errIdleSentinel, func() {
		t.Fatalf("onIdle should not fire when timeout=0")
	})
	defer ir.Stop()

	buf := make([]byte, 1)
	n, err := ir.Read(buf)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 byte, got %d", n)
	}
}
