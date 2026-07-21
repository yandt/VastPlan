package catalog

import (
	"errors"
	"fmt"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	LifecycleActive     = "active"
	LifecycleDeprecated = "deprecated"
	LifecycleYanked     = "yanked"
	LifecycleRevoked    = "revoked"
)

type LifecycleTransition struct {
	Revision    uint64                        `json:"revision"`
	Status      string                        `json:"status"`
	Reason      string                        `json:"reason"`
	Replacement *pluginv1.ArtifactRequirement `json:"replacement,omitempty"`
	OccurredAt  time.Time                     `json:"occurredAt"`
}

type LifecycleRequest struct {
	Ref              pluginv1.ArtifactRef          `json:"ref"`
	Status           string                        `json:"status"`
	Reason           string                        `json:"reason"`
	Replacement      *pluginv1.ArtifactRequirement `json:"replacement,omitempty"`
	ExpectedRevision uint64                        `json:"expectedRevision"`
}

func (s *Store) SetLifecycle(request LifecycleRequest, occurredAt time.Time) (Entry, uint64, error) {
	if s == nil {
		return Entry{}, 0, errors.New("Catalog 不可用")
	}
	request.Reason = strings.TrimSpace(request.Reason)
	if !validLifecycleStatus(request.Status) || request.Reason == "" || len([]rune(request.Reason)) > 500 {
		return Entry{}, 0, errors.New("生命周期状态或原因无效")
	}
	if request.Replacement != nil {
		if request.Status != LifecycleDeprecated || !capabilityPattern.MatchString(request.Replacement.PluginID) {
			return Entry{}, 0, errors.New("只有 deprecated 状态可声明合法替代制品")
		}
		if _, err := semver.NewConstraint(request.Replacement.Constraint); err != nil {
			return Entry{}, 0, fmt.Errorf("替代制品版本约束无效: %w", err)
		}
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.ExpectedRevision == 0 || request.ExpectedRevision != s.revision {
		return Entry{}, s.revision, fmt.Errorf("Catalog revision 冲突: expected=%d actual=%d", request.ExpectedRevision, s.revision)
	}
	key := refKey(request.Ref)
	entry, ok := s.entries[key]
	if !ok {
		return Entry{}, s.revision, errors.New("制品引用不存在")
	}
	previous := entry.LifecycleStatus
	if previous == "" {
		previous = LifecycleActive
	}
	if previous == LifecycleRevoked {
		return Entry{}, s.revision, errors.New("revoked 是不可逆安全状态")
	}
	if previous == request.Status {
		if entry.LifecycleReason == request.Reason && sameRequirement(entry.Replacement, request.Replacement) {
			return entry, s.revision, nil
		}
		return Entry{}, s.revision, errors.New("相同生命周期状态不能改写既有审计原因")
	}
	event := Event{Type: "artifact.lifecycle", Ref: entry.Ref, SHA256: entry.SHA256, OccurredAt: occurredAt.UTC(), PreviousStatus: previous, Status: request.Status, Reason: request.Reason, Replacement: cloneRequirement(request.Replacement)}
	if err := s.appendEventLocked(&event); err != nil {
		return Entry{}, s.revision, err
	}
	transition := LifecycleTransition{Revision: event.Revision, Status: request.Status, Reason: request.Reason, Replacement: cloneRequirement(request.Replacement), OccurredAt: event.OccurredAt}
	s.lifecycle[key] = append(s.lifecycle[key], transition)
	applyLifecycle(&entry, transition)
	s.entries[key] = entry
	if err := s.writeSnapshotLocked(); err != nil {
		return Entry{}, s.revision, err
	}
	return entry, s.revision, nil
}

func (s *Store) RequireDelivery(ref pluginv1.ArtifactRef) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entry, ok := s.entries[refKey(ref)]
	if !ok {
		return errors.New("制品引用不存在")
	}
	switch entry.LifecycleStatus {
	case LifecycleYanked:
		return errors.New("制品已 yanked，禁止新的交付")
	case LifecycleRevoked:
		return errors.New("制品已 revoked，验证与交付已撤销")
	default:
		return nil
	}
}

// ValidateKnownReferences allows a trusted consumer's complete snapshot to
// include artifacts resolved from another verified source such as Seed. When
// this Catalog does know the immutable ref, its digest must still match.
func (s *Store) ValidateKnownReferences(values []pluginv1.ArtifactReference) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, value := range values {
		entry, ok := s.entries[refKey(value.Ref)]
		if !ok {
			continue
		}
		if entry.SHA256 != value.SHA256 {
			return fmt.Errorf("引用快照包含摘要不匹配的已知制品: %s", refKey(value.Ref))
		}
	}
	return nil
}

func validLifecycleStatus(status string) bool {
	switch status {
	case LifecycleActive, LifecycleDeprecated, LifecycleYanked, LifecycleRevoked:
		return true
	default:
		return false
	}
}

func currentLifecycleStatus(history []LifecycleTransition) string {
	if len(history) == 0 {
		return LifecycleActive
	}
	return history[len(history)-1].Status
}

func lifecycleAt(history []LifecycleTransition, revision uint64) LifecycleTransition {
	result := LifecycleTransition{Status: LifecycleActive}
	for _, transition := range history {
		if transition.Revision > revision {
			break
		}
		result = transition
	}
	return result
}

func applyLifecycle(entry *Entry, transition LifecycleTransition) {
	entry.LifecycleStatus = transition.Status
	if entry.LifecycleStatus == "" {
		entry.LifecycleStatus = LifecycleActive
	}
	entry.LifecycleRevision = transition.Revision
	entry.LifecycleReason = transition.Reason
	entry.Replacement = cloneRequirement(transition.Replacement)
}

func cloneRequirement(value *pluginv1.ArtifactRequirement) *pluginv1.ArtifactRequirement {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func sameRequirement(left, right *pluginv1.ArtifactRequirement) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}
