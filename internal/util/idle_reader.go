package util

import (
	"io"
	"sync"
	"time"
)

// IdleReader wraps an io.Reader and enforces an idle-read timeout. The
// internal timer is reset on every successful (n>0) Read; if the timer
// expires before another read completes, onIdle is invoked and subsequent
// Read calls return the error supplied by onIdle's caller via the wrapper's
// state. The zero timeout disables the timer (behaves as a plain pass-through).
type IdleReader struct {
	r       io.Reader
	timeout time.Duration
	idleErr error
	onIdle  func()
	mu      sync.Mutex
	timer   *time.Timer
	fired   bool
	stopped bool
}

// NewIdleReader constructs an IdleReader. onIdle is called once, asynchronously,
// from the timer goroutine when the idle window elapses — typically used to
// cancel a context so the underlying Read unblocks. idleErr is substituted for
// any error returned by the underlying reader after the timer has fired so
// callers can distinguish idle timeout from generic network/context errors.
func NewIdleReader(r io.Reader, timeout time.Duration, idleErr error, onIdle func()) *IdleReader {
	ir := &IdleReader{
		r:       r,
		timeout: timeout,
		idleErr: idleErr,
		onIdle:  onIdle,
	}
	if timeout > 0 {
		ir.timer = time.AfterFunc(timeout, ir.fire)
	}
	return ir
}

// Read implements io.Reader.
func (ir *IdleReader) Read(p []byte) (int, error) {
	if ir.hasFired() {
		return 0, ir.idleErr
	}
	n, err := ir.r.Read(p)
	if n > 0 {
		ir.mu.Lock()
		if !ir.fired && !ir.stopped && ir.timer != nil {
			ir.timer.Reset(ir.timeout)
		}
		ir.mu.Unlock()
	}
	if err != nil && ir.hasFired() {
		err = ir.idleErr
	}
	return n, err
}

// Stop halts the idle timer. Safe to call multiple times.
func (ir *IdleReader) Stop() {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	ir.stopped = true
	if ir.timer != nil {
		ir.timer.Stop()
	}
}

func (ir *IdleReader) fire() {
	ir.mu.Lock()
	if ir.stopped {
		ir.mu.Unlock()
		return
	}
	ir.fired = true
	cb := ir.onIdle
	ir.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (ir *IdleReader) hasFired() bool {
	ir.mu.Lock()
	defer ir.mu.Unlock()
	return ir.fired
}
