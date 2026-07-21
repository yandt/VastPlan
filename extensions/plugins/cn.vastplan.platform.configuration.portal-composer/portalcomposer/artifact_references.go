package portalcomposer

import (
	"context"
	"fmt"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func (s *Service) protectPortalTransition(ctx context.Context, activationID uint64, portalID string, previous, candidate []pluginv1.ArtifactReference) error {
	values, err := mergePortalReferences(withPortalPurpose(previous, "active"), withPortalPurpose(candidate, "candidate"))
	if err != nil {
		return err
	}
	return s.publishReferenceSnapshot(ctx, pluginv1.ArtifactReferenceSnapshot{
		OwnerKind:  artifactreference.OwnerPortalActivation,
		OwnerID:    "portal/" + portalID,
		Generation: activationID*2 - 1,
		References: values,
	})
}

func (s *Service) publishPortalReferences(ctx context.Context, activation portalapi.PortalActivation, previous []pluginv1.ArtifactReference) error {
	ownerID := "portal/" + activation.PortalID
	if err := s.publishReferenceSnapshot(ctx, pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerRollbackHistory,
		OwnerID:   ownerID, Generation: activation.ID,
		References: withPortalPurpose(previous, "rollback"),
	}); err != nil {
		return err
	}
	return s.publishReferenceSnapshot(ctx, pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerPortalActivation,
		OwnerID:   ownerID, Generation: activation.ID * 2,
		References: withPortalPurpose(activation.ArtifactReferences, "active"),
	})
}

func (s *Service) restorePortalActiveReferences(ctx context.Context, activationID uint64, portalID string, previous []pluginv1.ArtifactReference) error {
	return s.publishReferenceSnapshot(ctx, pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerPortalActivation,
		OwnerID:   "portal/" + portalID, Generation: activationID * 2,
		References: withPortalPurpose(previous, "active"),
	})
}

func (s *Service) publishPortalTestReleaseReference(ctx context.Context, release portalapi.TestRelease) error {
	return s.publishReferenceSnapshot(ctx, pluginv1.ArtifactReferenceSnapshot{
		OwnerKind:  artifactreference.OwnerArtifactLock,
		OwnerID:    fmt.Sprintf("portal/test-release-%d", release.ID),
		Generation: 1,
		References: []pluginv1.ArtifactReference{{Ref: release.Artifact, SHA256: release.SHA256, Purpose: "test-release"}},
	})
}

// reconcilePortalReferences drains the durable activation reference outbox.
// Only the latest current Activation per Portal is eligible, so a stale
// controller cannot contract a newer owner's protection snapshot.
func (s *Service) reconcilePortalReferences(ctx context.Context, principal portalapi.Principal) error {
	if principal.ID == "" || principal.TenantID == "" {
		return ErrForbidden
	}
	type pendingReference struct {
		activation   portalapi.PortalActivation
		previous     []pluginv1.ArtifactReference
		previousSpec *portalapi.PortalSpec
	}
	s.mu.Lock()
	latest := map[string]uint64{}
	for _, activation := range s.state.Activations {
		if activation.TenantID == principal.TenantID && activation.Status == portalapi.ActivationCurrent && activation.ID > latest[activation.PortalID] {
			latest[activation.PortalID] = activation.ID
		}
	}
	pending := make([]pendingReference, 0)
	for _, activation := range s.state.Activations {
		if activation.TenantID != principal.TenantID || latest[activation.PortalID] != activation.ID || !activation.ReferencePending {
			continue
		}
		var previous []pluginv1.ArtifactReference
		var previousSpec *portalapi.PortalSpec
		for _, candidate := range s.state.Activations {
			if candidate.TenantID == principal.TenantID && candidate.ID == activation.PreviousActivationID {
				previous = append([]pluginv1.ArtifactReference(nil), candidate.ArtifactReferences...)
				spec := cloneSpec(candidate.Spec)
				previousSpec = &spec
				break
			}
		}
		pending = append(pending, pendingReference{activation: cloneJSON(activation), previous: previous, previousSpec: previousSpec})
	}
	s.mu.Unlock()

	for _, item := range pending {
		if len(item.activation.ArtifactReferences) == 0 {
			references, err := s.materializeCatalog(ctx, principal.TenantID, item.activation.Spec)
			if err != nil {
				return err
			}
			item.activation.ArtifactReferences = withPortalPurpose(references, "active")
		}
		if len(item.previous) == 0 && item.previousSpec != nil {
			references, err := s.materializeCatalog(ctx, principal.TenantID, *item.previousSpec)
			if err != nil {
				return err
			}
			item.previous = withPortalPurpose(references, "rollback")
		}
		if err := s.publishPortalReferences(ctx, item.activation, item.previous); err != nil {
			return err
		}
		s.mu.Lock()
		for i := range s.state.Activations {
			activation := &s.state.Activations[i]
			if activation.TenantID == principal.TenantID && activation.ID == item.activation.ID && activation.ReferencePending && s.currentActivationIDLocked(principal.TenantID, activation.PortalID) == activation.ID {
				activation.ArtifactReferences = append([]pluginv1.ArtifactReference(nil), item.activation.ArtifactReferences...)
				activation.ReferencePending = false
				if err := s.save(); err != nil {
					activation.ReferencePending = true
					s.mu.Unlock()
					return err
				}
				break
			}
		}
		s.mu.Unlock()
	}
	return nil
}

func (s *Service) markCurrentReferencesPendingLocked() bool {
	latest := map[string]uint64{}
	for _, activation := range s.state.Activations {
		if activation.Status == portalapi.ActivationCurrent {
			key := activation.TenantID + "\x00" + activation.PortalID
			if activation.ID > latest[key] {
				latest[key] = activation.ID
			}
		}
	}
	changed := false
	for i := range s.state.Activations {
		activation := &s.state.Activations[i]
		key := activation.TenantID + "\x00" + activation.PortalID
		if latest[key] == activation.ID && !activation.ReferencePending {
			activation.ReferencePending = true
			changed = true
		}
	}
	return changed
}

func withPortalPurpose(values []pluginv1.ArtifactReference, purpose string) []pluginv1.ArtifactReference {
	out := make([]pluginv1.ArtifactReference, len(values))
	for i, value := range values {
		out[i] = value
		if out[i].Ref.Channel == "" {
			out[i].Ref.Channel = "stable"
		}
		out[i].Purpose = purpose
	}
	return out
}

func mergePortalReferences(left, right []pluginv1.ArtifactReference) ([]pluginv1.ArtifactReference, error) {
	byRef := map[pluginv1.ArtifactRef]pluginv1.ArtifactReference{}
	for _, value := range append(append([]pluginv1.ArtifactReference(nil), left...), right...) {
		prior, exists := byRef[value.Ref]
		if exists && prior.SHA256 != value.SHA256 {
			return nil, fmt.Errorf("Portal 精确制品引用 %s@%s/%s 对应多个 SHA-256", value.Ref.PluginID, value.Ref.Version, value.Ref.Channel)
		}
		byRef[value.Ref] = value
	}
	out := make([]pluginv1.ArtifactReference, 0, len(byRef))
	for _, value := range byRef {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Ref.PluginID != out[j].Ref.PluginID {
			return out[i].Ref.PluginID < out[j].Ref.PluginID
		}
		if out[i].Ref.Version != out[j].Ref.Version {
			return out[i].Ref.Version < out[j].Ref.Version
		}
		return out[i].Ref.Channel < out[j].Ref.Channel
	})
	return out, nil
}
