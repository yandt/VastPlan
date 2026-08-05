package interaction

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

func (w *Workflow) Open(ctx context.Context, source interactionapi.Subject, request uiv1.InteractionRequest) (interactionapi.Record, error) {
	if !validSubject(source) || request.TenantID != source.TenantID || request.Source.Capability != source.ID {
		return interactionapi.Record{}, ErrForbidden
	}
	if err := uiv1.ValidateInteractionRequest(request); err != nil {
		return interactionapi.Record{}, err
	}
	now := w.service.now().UTC()
	if !request.ExpiresAt.After(now) {
		return interactionapi.Record{}, ErrExpired
	}
	hash, err := requestHash(request)
	if err != nil {
		return interactionapi.Record{}, err
	}
	if existing, err := w.repository.Get(ctx, source.TenantID, request.ID); err == nil {
		if existing.RequestHash == hash {
			return copyRecord(existing.Record), nil
		}
		return interactionapi.Record{}, ErrConflict
	} else if !errors.Is(err, ErrNotFound) {
		return interactionapi.Record{}, err
	}
	record := interactionapi.Record{Request: request, State: interactionapi.StateCreated, CreatedAt: now, UpdatedAt: now}
	record.Audit = append(record.Audit, interactionapi.AuditEvent{Action: "created", ActorID: source.ID, At: now})
	created, err := w.repository.Create(ctx, storedRecord{Record: record, RequestHash: hash}, "open:"+request.ID+":"+hash)
	if errors.Is(err, ErrConflict) {
		existing, getErr := w.repository.Get(ctx, source.TenantID, request.ID)
		if getErr == nil && existing.RequestHash == hash {
			return copyRecord(existing.Record), nil
		}
	}
	return copyRecord(created.Record), err
}

func (w *Workflow) List(ctx context.Context, subject interactionapi.Subject, surface uiv1.InteractionSurface) ([]interactionapi.Record, error) {
	if !validSubject(subject) || !validSurface(surface) {
		return nil, ErrForbidden
	}
	stored, err := w.repository.List(ctx, subject.TenantID)
	if err != nil {
		return nil, err
	}
	now := w.service.now().UTC()
	result := make([]interactionapi.Record, 0, len(stored))
	for _, candidate := range stored {
		candidate, err = w.expire(ctx, candidate, now)
		if err != nil {
			return nil, err
		}
		record := candidate.Record
		if !record.State.Terminal() && allowsSurface(record.Request, surface) && eligible(record.Request, subject) {
			result = append(result, copyRecord(record))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	return result, nil
}

func (w *Workflow) Get(ctx context.Context, subject interactionapi.Subject, id string) (interactionapi.Record, error) {
	if !validSubject(subject) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	stored, err := w.repository.Get(ctx, subject.TenantID, id)
	if err != nil {
		return interactionapi.Record{}, err
	}
	stored, err = w.expire(ctx, stored, w.service.now().UTC())
	if err != nil {
		return interactionapi.Record{}, err
	}
	if !subject.System && stored.Request.Source.Capability != subject.ID && !eligible(stored.Request, subject) {
		return interactionapi.Record{}, ErrNotFound
	}
	return copyRecord(stored.Record), nil
}

// Watch is a reconnect-safe long-poll primitive. Database state is always
// re-read after registering a local wake-up to close the check/wait race.
func (w *Workflow) Watch(ctx context.Context, source interactionapi.Subject, id string, after time.Time) (interactionapi.Record, error) {
	if !validSubject(source) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	for {
		stored, err := w.repository.Get(ctx, source.TenantID, id)
		if err != nil {
			return interactionapi.Record{}, err
		}
		if !source.System && stored.Request.Source.Capability != source.ID {
			return interactionapi.Record{}, ErrForbidden
		}
		stored, err = w.expire(ctx, stored, w.service.now().UTC())
		if err != nil {
			return interactionapi.Record{}, err
		}
		if stored.State.Terminal() || stored.UpdatedAt.After(after) {
			return copyRecord(stored.Record), nil
		}
		wait := w.wait(id)
		latest, readErr := w.repository.Get(ctx, source.TenantID, id)
		if readErr != nil || latest.Revision != stored.Revision {
			w.removeWatcher(id, wait)
			if readErr != nil {
				return interactionapi.Record{}, readErr
			}
			continue
		}
		timer := time.NewTimer(stored.Request.ExpiresAt.Sub(w.service.now().UTC()))
		select {
		case <-ctx.Done():
			stopTimer(timer)
			w.removeWatcher(id, wait)
			return interactionapi.Record{}, ctx.Err()
		case <-wait:
			stopTimer(timer)
		case <-timer.C:
			w.removeWatcher(id, wait)
		}
	}
}

func (w *Workflow) Present(ctx context.Context, subject interactionapi.Subject, id string, surface uiv1.InteractionSurface) (interactionapi.Record, error) {
	return w.mutateRenderer(ctx, subject, id, surface, "present", func(record *interactionapi.Record, now time.Time) error {
		if record.State != interactionapi.StateCreated && record.State != interactionapi.StatePresented {
			return ErrInvalidState
		}
		record.State, record.PresentedBy, record.UpdatedAt = interactionapi.StatePresented, subject.ID, now
		record.Audit = append(record.Audit, interactionapi.AuditEvent{Action: "presented", ActorID: subject.ID, Surface: string(surface), At: now})
		return nil
	})
}

func (w *Workflow) Respond(ctx context.Context, subject interactionapi.Subject, id string, surface uiv1.InteractionSurface, response uiv1.InteractionResponse) (interactionapi.Record, error) {
	if response.InteractionID != id || response.Decision != uiv1.DecisionAnswered && response.Decision != uiv1.DecisionRejected {
		return interactionapi.Record{}, ErrInvalidState
	}
	return w.mutateRenderer(ctx, subject, id, surface, "respond", func(record *interactionapi.Record, now time.Time) error {
		if record.State != interactionapi.StateCreated && record.State != interactionapi.StatePresented {
			return ErrConflict
		}
		if err := validateResponse(record.Request, response); err != nil {
			return err
		}
		responseCopy := copyResponse(response)
		record.Response = &responseCopy
		if response.Decision == uiv1.DecisionAnswered {
			record.State = interactionapi.StateAnswered
		} else {
			record.State = interactionapi.StateRejected
		}
		record.UpdatedAt = now
		record.Audit = append(record.Audit, interactionapi.AuditEvent{Action: string(record.State), ActorID: subject.ID, Surface: string(surface), At: now})
		return nil
	})
}

func (w *Workflow) Cancel(ctx context.Context, source interactionapi.Subject, id string) (interactionapi.Record, error) {
	if !validSubject(source) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	stored, err := w.repository.Get(ctx, source.TenantID, id)
	if err != nil {
		return interactionapi.Record{}, err
	}
	if !source.System && stored.Request.Source.Capability != source.ID {
		return interactionapi.Record{}, ErrForbidden
	}
	stored, err = w.expire(ctx, stored, w.service.now().UTC())
	if err != nil {
		return interactionapi.Record{}, err
	}
	if stored.State.Terminal() {
		return interactionapi.Record{}, ErrConflict
	}
	now := w.service.now().UTC()
	stored.State, stored.UpdatedAt = interactionapi.StateCancelled, now
	stored.Audit = append(stored.Audit, interactionapi.AuditEvent{Action: "cancelled", ActorID: source.ID, At: now})
	updated, err := w.repository.Update(ctx, stored, stored.Revision, mutationKey("cancel", id, stored.Revision))
	if err != nil {
		return interactionapi.Record{}, err
	}
	w.notify(id)
	return copyRecord(updated.Record), nil
}

func (w *Workflow) mutateRenderer(ctx context.Context, subject interactionapi.Subject, id string, surface uiv1.InteractionSurface,
	action string, mutate func(*interactionapi.Record, time.Time) error) (interactionapi.Record, error) {
	if !validSubject(subject) || !validSurface(surface) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	stored, err := w.repository.Get(ctx, subject.TenantID, id)
	if err != nil {
		return interactionapi.Record{}, err
	}
	if !allowsSurface(stored.Request, surface) || !eligible(stored.Request, subject) {
		return interactionapi.Record{}, ErrNotFound
	}
	stored, err = w.expire(ctx, stored, w.service.now().UTC())
	if err != nil {
		return interactionapi.Record{}, err
	}
	if stored.State == interactionapi.StateExpired {
		return interactionapi.Record{}, ErrExpired
	}
	if err := mutate(&stored.Record, w.service.now().UTC()); err != nil {
		return interactionapi.Record{}, err
	}
	updated, err := w.repository.Update(ctx, stored, stored.Revision, mutationKey(action, id, stored.Revision))
	if err != nil {
		return interactionapi.Record{}, err
	}
	w.notify(id)
	return copyRecord(updated.Record), nil
}

func (w *Workflow) expire(ctx context.Context, stored storedRecord, now time.Time) (storedRecord, error) {
	if stored.State.Terminal() || stored.Request.ExpiresAt.After(now) {
		return stored, nil
	}
	stored.State, stored.UpdatedAt = interactionapi.StateExpired, now
	stored.Audit = append(stored.Audit, interactionapi.AuditEvent{Action: "expired", ActorID: "system", At: now})
	updated, err := w.repository.Update(ctx, stored, stored.Revision, mutationKey("expire", stored.Request.ID, stored.Revision))
	if err != nil {
		if errors.Is(err, ErrConflict) {
			return w.repository.Get(ctx, stored.Request.TenantID, stored.Request.ID)
		}
		return storedRecord{}, err
	}
	w.notify(stored.Request.ID)
	return updated, nil
}

func mutationKey(action, id string, revision int64) string {
	return fmt.Sprintf("%s:%s:%d", action, id, revision)
}

func stopTimer(timer *time.Timer) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
}

func validSurface(surface uiv1.InteractionSurface) bool {
	return surface == uiv1.SurfaceFrontend || surface == uiv1.SurfaceMobile || surface == uiv1.SurfaceRunnerLocal
}

func allowsSurface(request uiv1.InteractionRequest, surface uiv1.InteractionSurface) bool {
	for _, allowed := range request.AllowedSurfaces {
		if allowed == surface {
			return true
		}
	}
	return false
}

func eligible(request uiv1.InteractionRequest, subject interactionapi.Subject) bool {
	for _, candidate := range request.EligibleSubjects {
		if candidate == subject.ID {
			return true
		}
		if role, ok := strings.CutPrefix(candidate, "role:"); ok {
			for _, subjectRole := range subject.Roles {
				if role == subjectRole {
					return true
				}
			}
		}
	}
	return false
}
