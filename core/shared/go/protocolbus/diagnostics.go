package protocolbus

import (
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/observability"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocollimit"
)

type SessionDiagnostic struct {
	PluginID  string        `json:"plugin_id"`
	Version   string        `json:"version"`
	SessionID string        `json:"session_id"`
	Alive     bool          `json:"alive"`
	Pending   int           `json:"pending"`
	IdleFor   time.Duration `json:"idle_for"`
}

type DiagnosticSnapshot struct {
	Kernel     string                 `json:"kernel"`
	Version    string                 `json:"version"`
	Healthy    bool                   `json:"healthy"`
	Ready      bool                   `json:"ready"`
	Draining   bool                   `json:"draining"`
	Inflight   int                    `json:"inflight"`
	Limits     protocollimit.Limits   `json:"limits"`
	Sessions   []SessionDiagnostic    `json:"sessions"`
	Metrics    observability.Snapshot `json:"metrics"`
	CapturedAt time.Time              `json:"captured_at"`
}

func (h *Host) Healthy() bool { return h != nil && h.srv != nil && !h.stopped.Load() }

func (h *Host) Ready() bool {
	if !h.Healthy() {
		return false
	}
	h.callMu.Lock()
	defer h.callMu.Unlock()
	return !h.draining
}

// DiagnosticSnapshot 返回无敏感 payload 的一致诊断副本，可供健康端点或 support bundle 使用。
func (h *Host) DiagnosticSnapshot() DiagnosticSnapshot {
	h.callMu.Lock()
	draining, inflight := h.draining, h.inflight
	h.callMu.Unlock()
	h.mu.RLock()
	sessions := make([]*session, 0, len(h.sessions))
	for _, session := range h.sessions {
		sessions = append(sessions, session)
	}
	h.mu.RUnlock()
	out := DiagnosticSnapshot{Kernel: h.KernelName, Version: h.KernelVersion, Healthy: h.Healthy(), Ready: h.Healthy() && !draining, Draining: draining, Inflight: inflight, Limits: h.limits(), CapturedAt: time.Now().UTC()}
	for _, session := range sessions {
		session.pendingMu.Lock()
		pending := len(session.pending)
		session.pendingMu.Unlock()
		out.Sessions = append(out.Sessions, SessionDiagnostic{PluginID: session.pluginID, Version: session.pluginVersion, SessionID: session.id, Alive: !isClosed(session.done), Pending: pending, IdleFor: session.idleFor()})
	}
	sort.Slice(out.Sessions, func(i, j int) bool { return out.Sessions[i].PluginID < out.Sessions[j].PluginID })
	if h.Observer != nil {
		h.Observer.Metrics.SetGauge("kernel_inflight_calls", int64(inflight), nil)
		h.Observer.Metrics.SetGauge("kernel_plugin_sessions", int64(len(out.Sessions)), nil)
		out.Metrics = h.Observer.Snapshot()
	}
	return out
}

func isClosed(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}
