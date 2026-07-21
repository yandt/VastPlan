package edge

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

type PortalUpdate struct {
	TenantID   string `json:"-"`
	PortalID   string `json:"portalId"`
	Activation uint64 `json:"activationId"`
	Mode       string `json:"mode"`
}

type portalUpdateSubscription struct {
	tenantID, portalID string
	events             chan PortalUpdate
}

// PortalUpdateHub is an Edge-local, lossy notification fanout. Activations
// remain the durable truth; reconnecting browsers always receive the current
// revision before listening for newer events.
type PortalUpdateHub struct {
	mu          sync.Mutex
	next        uint64
	subscribers map[uint64]portalUpdateSubscription
}

func NewPortalUpdateHub() *PortalUpdateHub {
	return &PortalUpdateHub{subscribers: map[uint64]portalUpdateSubscription{}}
}

func (h *PortalUpdateHub) Publish(update PortalUpdate) {
	if h == nil || update.TenantID == "" || update.PortalID == "" || update.Activation == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, subscriber := range h.subscribers {
		if subscriber.tenantID != update.TenantID || subscriber.portalID != update.PortalID {
			continue
		}
		select {
		case subscriber.events <- update:
		default:
			select {
			case <-subscriber.events:
			default:
			}
			select {
			case subscriber.events <- update:
			default:
			}
		}
	}
}

func (h *PortalUpdateHub) subscribe(tenantID, portalID string) (<-chan PortalUpdate, func()) {
	h.mu.Lock()
	h.next++
	id := h.next
	subscriber := portalUpdateSubscription{tenantID: tenantID, portalID: portalID, events: make(chan PortalUpdate, 1)}
	h.subscribers[id] = subscriber
	h.mu.Unlock()
	return subscriber.events, func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
}

func classifyPortalUpdate(previous, current portalapi.PortalSpec) string {
	if previous.RenderAdapter.ID != current.RenderAdapter.ID || previous.RenderAdapter.Version != current.RenderAdapter.Version ||
		previous.RenderAdapter.Channel != current.RenderAdapter.Channel || previous.RenderAdapter.UIContract != current.RenderAdapter.UIContract ||
		previous.RenderAdapter.Config.DefaultRenderer != current.RenderAdapter.Config.DefaultRenderer ||
		previous.Shell.UIContract != current.Shell.UIContract || previous.Workbench.UIContract != current.Workbench.UIContract {
		return "host-epoch"
	}
	return "generation"
}

func (h *Handler) portalUpdates(w http.ResponseWriter, r *http.Request, principal portalapi.Principal, activations []portalapi.PortalActivation) {
	if h.updates == nil || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	activation, ok := selectActivePortal(activations, principal.TenantID, r.URL.Query().Get("path"), requestHost(r))
	if !ok {
		writeError(w, http.StatusNotFound, "portal_not_found")
		return
	}
	if !audienceAllows(activation.Spec.Audience, principal) {
		writeError(w, http.StatusForbidden, "portal_audience_forbidden")
		return
	}
	if activation.Spec.Updates.Mode == "refresh" {
		http.NotFound(w, r)
		return
	}
	clientRevision, err := strconv.ParseUint(r.URL.Query().Get("revision"), 10, 64)
	if err != nil || clientRevision == 0 || clientRevision > activation.ID {
		writeError(w, http.StatusBadRequest, "portal_update_revision_invalid")
		return
	}
	events, cancel := h.updates.subscribe(principal.TenantID, activation.PortalID)
	defer cancel()
	// Subscribe before the second read so an Activation committed in this
	// window is either in the durable snapshot or queued in the lossy channel.
	latest, err := h.service.ListActivations(r.Context(), principal)
	if err != nil {
		respond(w, nil, err)
		return
	}
	activation, ok = selectActivePortal(latest, principal.TenantID, r.URL.Query().Get("path"), requestHost(r))
	if !ok {
		writeError(w, http.StatusNotFound, "portal_not_found")
		return
	}
	initialMode := "current"
	if activation.ID > clientRevision {
		initialMode = "host-epoch"
		for _, previous := range latest {
			if previous.TenantID == principal.TenantID && previous.PortalID == activation.PortalID && previous.ID == clientRevision {
				initialMode = classifyPortalUpdate(previous.Spec, activation.Spec)
				break
			}
		}
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "portal_updates_unavailable")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	writePortalUpdateEvent(w, PortalUpdate{PortalID: activation.PortalID, Activation: activation.ID, Mode: initialMode})
	flusher.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case update := <-events:
			writePortalUpdateEvent(w, update)
			flusher.Flush()
		case <-heartbeat.C:
			_, _ = w.Write([]byte(": heartbeat\n\n"))
			flusher.Flush()
		}
	}
}

func writePortalUpdateEvent(w http.ResponseWriter, update PortalUpdate) {
	raw, _ := json.Marshal(update)
	_, _ = w.Write([]byte("event: activation\ndata: "))
	_, _ = w.Write(raw)
	_, _ = w.Write([]byte("\n\n"))
}
