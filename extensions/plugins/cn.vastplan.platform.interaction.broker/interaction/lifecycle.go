package interaction

import (
	"context"
	"sort"
	"strings"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

func (s *Service) Open(_ context.Context, source interactionapi.Subject, request uiv1.InteractionRequest) (interactionapi.Record, error) {
	if !validSubject(source) || request.TenantID != source.TenantID || request.Source.Capability != source.ID {
		return interactionapi.Record{}, ErrForbidden
	}
	if err := uiv1.ValidateInteractionRequest(request); err != nil {
		return interactionapi.Record{}, err
	}
	now := s.now().UTC()
	if !request.ExpiresAt.After(now) {
		return interactionapi.Record{}, ErrExpired
	}
	hash, err := requestHash(request)
	if err != nil {
		return interactionapi.Record{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.state.Records[request.ID]; ok {
		if existing.RequestHash == hash {
			return copyRecord(existing.Record), nil
		}
		return interactionapi.Record{}, ErrConflict
	}
	record := interactionapi.Record{Request: request, State: interactionapi.StateCreated, CreatedAt: now, UpdatedAt: now}
	record.Audit = append(record.Audit, interactionapi.AuditEvent{Action: "created", ActorID: source.ID, At: now})
	s.state.Records[request.ID] = storedRecord{Record: record, RequestHash: hash}
	if err := s.save(); err != nil {
		delete(s.state.Records, request.ID)
		return interactionapi.Record{}, err
	}
	return copyRecord(record), nil
}

func (s *Service) List(_ context.Context, subject interactionapi.Subject, surface uiv1.InteractionSurface) ([]interactionapi.Record, error) {
	if !validSubject(subject) || !validSurface(surface) {
		return nil, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expireLocked(s.now().UTC())
	result := make([]interactionapi.Record, 0)
	for _, stored := range s.state.Records {
		record := stored.Record
		if record.Request.TenantID == subject.TenantID && !record.State.Terminal() && allowsSurface(record.Request, surface) && eligible(record.Request, subject) {
			result = append(result, copyRecord(record))
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].CreatedAt.Before(result[j].CreatedAt) })
	if err := s.save(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Service) Get(_ context.Context, subject interactionapi.Subject, id string) (interactionapi.Record, error) {
	if !validSubject(subject) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expired := s.expireLocked(s.now().UTC())
	stored, ok := s.state.Records[id]
	if !ok || stored.Request.TenantID != subject.TenantID || !(subject.System || stored.Request.Source.Capability == subject.ID || eligible(stored.Request, subject)) {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrNotFound
	}
	if err := s.save(); err != nil {
		return interactionapi.Record{}, err
	}
	return copyRecord(stored.Record), nil
}

// Watch is a reconnect-safe long-poll primitive for Runner and backend sources.
// The caller retains Record.UpdatedAt as its cursor; reconnecting with that
// cursor returns after a mutation, expiry, or context deadline without any
// dependency on Portal or Mobile runtime code.
func (s *Service) Watch(ctx context.Context, source interactionapi.Subject, id string, after time.Time) (interactionapi.Record, error) {
	if !validSubject(source) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	for {
		s.mu.Lock()
		now := s.now().UTC()
		expired := s.expireLocked(now)
		stored, ok := s.state.Records[id]
		if !ok || stored.Request.TenantID != source.TenantID {
			if expired {
				if err := s.save(); err != nil {
					s.mu.Unlock()
					return interactionapi.Record{}, err
				}
			}
			s.mu.Unlock()
			return interactionapi.Record{}, ErrNotFound
		}
		if !source.System && stored.Request.Source.Capability != source.ID {
			s.mu.Unlock()
			return interactionapi.Record{}, ErrForbidden
		}
		if expired {
			if err := s.save(); err != nil {
				s.mu.Unlock()
				return interactionapi.Record{}, err
			}
			s.notifyLocked(id)
			stored = s.state.Records[id]
		}
		if stored.State.Terminal() || stored.UpdatedAt.After(after) {
			record := copyRecord(stored.Record)
			s.mu.Unlock()
			return record, nil
		}
		wait := make(chan struct{})
		if s.watchers[id] == nil {
			s.watchers[id] = map[chan struct{}]struct{}{}
		}
		s.watchers[id][wait] = struct{}{}
		expiresIn := stored.Request.ExpiresAt.Sub(now)
		s.mu.Unlock()

		timer := time.NewTimer(expiresIn)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			s.removeWatcher(id, wait)
			return interactionapi.Record{}, ctx.Err()
		case <-wait:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
		case <-timer.C:
			s.removeWatcher(id, wait)
		}
	}
}

func (s *Service) Present(_ context.Context, subject interactionapi.Subject, id string, surface uiv1.InteractionSurface) (interactionapi.Record, error) {
	return s.mutateRenderer(subject, id, surface, func(record *interactionapi.Record, now time.Time) error {
		if record.State != interactionapi.StateCreated && record.State != interactionapi.StatePresented {
			return ErrInvalidState
		}
		record.State = interactionapi.StatePresented
		record.PresentedBy = subject.ID
		record.UpdatedAt = now
		record.Audit = append(record.Audit, interactionapi.AuditEvent{Action: "presented", ActorID: subject.ID, Surface: string(surface), At: now})
		return nil
	})
}

func (s *Service) Respond(_ context.Context, subject interactionapi.Subject, id string, surface uiv1.InteractionSurface, response uiv1.InteractionResponse) (interactionapi.Record, error) {
	if response.InteractionID != id || (response.Decision != uiv1.DecisionAnswered && response.Decision != uiv1.DecisionRejected) {
		return interactionapi.Record{}, ErrInvalidState
	}
	return s.mutateRenderer(subject, id, surface, func(record *interactionapi.Record, now time.Time) error {
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

func (s *Service) Cancel(_ context.Context, source interactionapi.Subject, id string) (interactionapi.Record, error) {
	if !validSubject(source) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	expired := s.expireLocked(now)
	stored, ok := s.state.Records[id]
	if !ok || stored.Request.TenantID != source.TenantID {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrNotFound
	}
	if !source.System && stored.Request.Source.Capability != source.ID {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrForbidden
	}
	if stored.State.Terminal() {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrConflict
	}
	stored.State = interactionapi.StateCancelled
	stored.UpdatedAt = now
	stored.Audit = append(stored.Audit, interactionapi.AuditEvent{Action: "cancelled", ActorID: source.ID, At: now})
	s.state.Records[id] = stored
	if err := s.save(); err != nil {
		return interactionapi.Record{}, err
	}
	s.notifyLocked(id)
	return copyRecord(stored.Record), nil
}

func (s *Service) mutateRenderer(subject interactionapi.Subject, id string, surface uiv1.InteractionSurface, mutate func(*interactionapi.Record, time.Time) error) (interactionapi.Record, error) {
	if !validSubject(subject) || !validSurface(surface) || strings.TrimSpace(id) == "" {
		return interactionapi.Record{}, ErrForbidden
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	expired := s.expireLocked(now)
	stored, ok := s.state.Records[id]
	if !ok || stored.Request.TenantID != subject.TenantID || !allowsSurface(stored.Request, surface) || !eligible(stored.Request, subject) {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrNotFound
	}
	if stored.State == interactionapi.StateExpired {
		if expired {
			if err := s.save(); err != nil {
				return interactionapi.Record{}, err
			}
		}
		return interactionapi.Record{}, ErrExpired
	}
	if err := mutate(&stored.Record, now); err != nil {
		return interactionapi.Record{}, err
	}
	s.state.Records[id] = stored
	if err := s.save(); err != nil {
		return interactionapi.Record{}, err
	}
	s.notifyLocked(id)
	return copyRecord(stored.Record), nil
}

func (s *Service) expireLocked(now time.Time) bool {
	changed := false
	for id, stored := range s.state.Records {
		if !stored.State.Terminal() && !stored.Request.ExpiresAt.After(now) {
			stored.State = interactionapi.StateExpired
			stored.UpdatedAt = now
			stored.Audit = append(stored.Audit, interactionapi.AuditEvent{Action: "expired", ActorID: "system", At: now})
			s.state.Records[id] = stored
			changed = true
		}
	}
	return changed
}

func (s *Service) notifyLocked(id string) {
	for wait := range s.watchers[id] {
		close(wait)
	}
	delete(s.watchers, id)
}

func (s *Service) removeWatcher(id string, wait chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if waiting := s.watchers[id]; waiting != nil {
		delete(waiting, wait)
		if len(waiting) == 0 {
			delete(s.watchers, id)
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
