package protocolbus

import (
	"context"
	"errors"
)

var ErrHostDraining = errors.New("插件宿主正在 drain，不再接受新调用")
var ErrConcurrencyLimited = errors.New("插件宿主并发调用达到上限")

func (h *Host) enterCall() error {
	h.callMu.Lock()
	defer h.callMu.Unlock()
	if h.draining {
		return ErrHostDraining
	}
	if h.inflight >= h.limits().MaxConcurrentCalls {
		return ErrConcurrencyLimited
	}
	h.inflight++
	return nil
}

func (h *Host) leaveCall() {
	h.callMu.Lock()
	defer h.callMu.Unlock()
	if h.inflight > 0 {
		h.inflight--
	}
	if h.draining && h.inflight == 0 {
		h.drainOnce.Do(func() { close(h.drainDone) })
	}
}

func (h *Host) beginDrain() <-chan struct{} {
	h.callMu.Lock()
	h.draining = true
	if h.inflight == 0 {
		h.drainOnce.Do(func() { close(h.drainDone) })
	}
	done := h.drainDone
	h.callMu.Unlock()
	return done
}

func (h *Host) waitForInflight(ctx context.Context) error {
	select {
	case <-h.beginDrain():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
