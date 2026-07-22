package authorizationpolicy

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"time"
)

var managedID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/~-]{0,159}$`)

func (s *Service) commit(state State, expected uint64, event AuditEvent) (State, error) {
	state.Generation = expected + 1
	state.Audit = append(state.Audit, event)
	if len(state.Audit) > 10_000 {
		state.Audit = append([]AuditEvent(nil), state.Audit[len(state.Audit)-10_000:]...)
	}
	return s.store.CompareAndSwap(expected, state)
}

func (s *Service) audit(subject, action, kind, id string, revision uint64, reason string) AuditEvent {
	return AuditEvent{ID: randomID("audit"), Action: action, ObjectKind: kind, ObjectID: id, Revision: revision, SubjectID: subject, Reason: reason, OccurredAt: s.now().UTC()}
}

func randomID(prefix string) string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		panic(err)
	}
	return prefix + "." + hex.EncodeToString(value)
}

func nextRoleRevision(state State, id string) (uint64, error) {
	var latest uint64
	for _, role := range state.Roles {
		if role.ID != id {
			continue
		}
		if role.State == StateDraft || role.State == StatePendingApproval || role.State == StateApproved {
			return 0, fmt.Errorf("Role %s 已有未结束 revision", id)
		}
		if role.Revision > latest {
			latest = role.Revision
		}
	}
	return latest + 1, nil
}

func nextBindingRevision(state State, id string) (uint64, error) {
	var latest uint64
	for _, binding := range state.Bindings {
		if binding.ID != id {
			continue
		}
		if binding.State == StateDraft || binding.State == StatePendingApproval || binding.State == StateApproved {
			return 0, fmt.Errorf("Binding %s 已有未结束 revision", id)
		}
		if binding.Revision > latest {
			latest = binding.Revision
		}
	}
	return latest + 1, nil
}

func ensureExpected(state State, expected uint64) error {
	if expected != state.Generation {
		return fmt.Errorf("expectedGeneration 冲突: expected=%d actual=%d", expected, state.Generation)
	}
	return nil
}

func validateManagedID(value, label string) error {
	if !managedID.MatchString(value) {
		return fmt.Errorf("%s 无效", label)
	}
	return nil
}

func validWindow(notBefore, expiresAt time.Time) error {
	if notBefore.IsZero() || !expiresAt.After(notBefore) || expiresAt.Sub(notBefore) > 365*24*time.Hour {
		return errors.New("Binding 有效时间窗无效或超过一年")
	}
	return nil
}
