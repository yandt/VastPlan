// Package interaction implements application workflows and authorization for
// cross-platform human interactions. Persistence is owned by a Repository;
// the service contains only clocks and local wake-up channels.
package interaction

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

const (
	PluginID      = "cn.vastplan.platform.interaction.broker"
	PluginVersion = "0.2.0"
	Capability    = interactionapi.Capability
)

var (
	ErrForbidden    = interactionapi.ErrForbidden
	ErrNotFound     = interactionapi.ErrNotFound
	ErrConflict     = interactionapi.ErrConflict
	ErrExpired      = interactionapi.ErrExpired
	ErrInvalidState = interactionapi.ErrInvalidState
)

type storedRecord struct {
	interactionapi.Record
	RequestHash string
	Revision    int64
}

type Repository interface {
	Create(ctx context.Context, record storedRecord, idempotencyKey string) (storedRecord, error)
	Get(ctx context.Context, tenantID, id string) (storedRecord, error)
	List(ctx context.Context, tenantID string) ([]storedRecord, error)
	Update(ctx context.Context, record storedRecord, expectedRevision int64, idempotencyKey string) (storedRecord, error)
}

type Service struct {
	now      func() time.Time
	watchMu  sync.Mutex
	watchers map[string]map[chan struct{}]struct{}
}

type Workflow struct {
	service    *Service
	repository Repository
}

func New() *Service {
	return &Service{now: time.Now, watchers: map[string]map[chan struct{}]struct{}{}}
}

func (s *Service) Workflow(repository Repository) *Workflow {
	return &Workflow{service: s, repository: repository}
}

func validSubject(subject interactionapi.Subject) bool {
	return strings.TrimSpace(subject.ID) != "" && strings.TrimSpace(subject.TenantID) != ""
}

func requestHash(request uiv1.InteractionRequest) (string, error) {
	raw, err := json.Marshal(request)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func (w *Workflow) notify(id string) {
	w.service.watchMu.Lock()
	defer w.service.watchMu.Unlock()
	for wait := range w.service.watchers[id] {
		close(wait)
	}
	delete(w.service.watchers, id)
}

func (w *Workflow) wait(id string) chan struct{} {
	w.service.watchMu.Lock()
	defer w.service.watchMu.Unlock()
	wait := make(chan struct{})
	if w.service.watchers[id] == nil {
		w.service.watchers[id] = map[chan struct{}]struct{}{}
	}
	w.service.watchers[id][wait] = struct{}{}
	return wait
}

func (w *Workflow) removeWatcher(id string, wait chan struct{}) {
	w.service.watchMu.Lock()
	defer w.service.watchMu.Unlock()
	if waiting := w.service.watchers[id]; waiting != nil {
		delete(waiting, wait)
		if len(waiting) == 0 {
			delete(w.service.watchers, id)
		}
	}
}
