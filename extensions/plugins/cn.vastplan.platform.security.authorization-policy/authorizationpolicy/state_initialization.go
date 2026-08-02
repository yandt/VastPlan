package authorizationpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

func cloneState(state *State) (*State, error) {
	if state == nil {
		return nil, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, err
	}
	var cloned State
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil, err
	}
	return &cloned, nil
}

func (s *Service) initialize(store Store) error {
	state, err := store.Load()
	if err != nil {
		return err
	}
	if state.Generation == 0 && s.bootstrapState != nil && s.bootstrapState.Generation > 0 {
		state = *s.bootstrapState
		state.Generation = 0
	}
	if state.Generation == 0 {
		if err := s.createInitialState(store, state); err != nil {
			return err
		}
		state, err = store.Load()
		if err != nil {
			return err
		}
	}
	previousGeneration := state.Generation
	now := s.now().UTC()
	materialChanged := state.Catalog.Digest != s.catalog.Digest || !bytes.Equal(mustJSON(state.ProviderProfile), mustJSON(s.providerProfile)) || !bytes.Equal(mustJSON(state.Domains), mustJSON(s.domains))
	seedChanged := false
	if materialChanged {
		// Reconcile only explicitly enabled Seed-owned objects before validating a
		// shrinking Catalog. User-managed roles still fail closed below.
		seedChanged, err = s.reconcileBootstrap(&state)
		if err != nil {
			return err
		}
		known := catalogPermissions(s.catalog)
		for _, role := range state.Roles {
			if role.State == StateRetired {
				continue
			}
			for _, statement := range role.Statements {
				for _, permission := range statement.Permissions {
					if _, exists := known[permission]; !exists {
						return fmt.Errorf("新权限目录移除了活动 Role %s 使用的权限 %s", role.ID, permission)
					}
				}
			}
		}
		state.Catalog = s.catalog
		state.ProviderProfile = s.providerProfile
		state.Domains = append([]authorizationv1.PolicyDomain(nil), s.domains...)
	} else {
		seedChanged, err = s.reconcileBootstrap(&state)
		if err != nil {
			return err
		}
	}
	if materialChanged && !seedChanged {
		state.Generation++
		state.Audit = append(state.Audit, AuditEvent{ID: randomID("audit"), Action: "catalogUpdate", ObjectKind: "catalog", ObjectID: s.catalog.Digest, Revision: state.Generation, SubjectID: "trusted-host", OccurredAt: now})
		_, err = store.CompareAndSwap(previousGeneration, state)
		return err
	}
	renewedBindings := s.leasePolicy.RenewManagedBindings(&state, now)
	renewSnapshot, err := snapshotLeaseRenewalRequired(state, s.leasePolicy, now)
	recoverSnapshot := false
	if err != nil {
		if !s.bootstrapReconciliation.AllowSnapshotRecovery() {
			return fmt.Errorf("%w；普通租约协调拒绝修复，必须使用显式 Bootstrap 策略", err)
		}
		renewSnapshot, recoverSnapshot = true, true
	}
	if seedChanged || len(renewedBindings) > 0 || renewSnapshot {
		if materialChanged {
			state.Audit = append(state.Audit, AuditEvent{ID: randomID("audit"), Action: "catalogUpdate", ObjectKind: "catalog", ObjectID: s.catalog.Digest, Revision: previousGeneration + 1, SubjectID: "trusted-host", OccurredAt: now})
		}
		action, reason := "snapshotLeaseRenewed", "signed snapshot lease reached its renewal boundary"
		if seedChanged {
			action, reason = "bootstrapSeedReconcile", "explicit bootstrap"
		} else if recoverSnapshot {
			action, reason = "snapshotAuthorityRecovered", "explicit bootstrap repaired authoritative state/snapshot drift"
		} else if len(renewedBindings) > 0 {
			action, reason = "managedBindingLeaseRenewed", "bindings="+strings.Join(renewedBindings, ",")
		}
		_, _, err := s.publishState(store, state, previousGeneration, s.leasePolicy.Audiences(), s.leasePolicy.SnapshotTTL(), s.audit("trusted-host", action, "authorization", "lease", state.PolicyRevision+1, reason))
		return err
	}
	return s.ensureSnapshotProjection(state.CurrentSnapshot)
}

func (s *Service) ensureSnapshotProjection(snapshot *authorizationv1.PolicySnapshot) error {
	if snapshot == nil {
		return nil
	}
	publication, err := s.signer.Sign(*snapshot)
	if err != nil {
		return fmt.Errorf("重建 Policy Snapshot 投影: %w", err)
	}
	reader, readable := s.snapshotWriter.(SnapshotReader)
	if readable {
		current, readErr := reader.Read()
		if readErr == nil {
			left, leftErr := authorizationv1.CanonicalPolicySnapshot(current.Payload)
			right, rightErr := authorizationv1.CanonicalPolicySnapshot(*snapshot)
			if leftErr == nil && rightErr == nil && bytes.Equal(left, right) && bytes.Equal(mustJSON(current.Signature), mustJSON(publication.Snapshot.Signature)) {
				return nil
			}
		}
	}
	if err := s.snapshotWriter.Write(publication.Snapshot); err != nil {
		return fmt.Errorf("重建 Policy Snapshot 投影: %w", err)
	}
	return nil
}

func (s *Service) reconcileBootstrap(state *State) (bool, error) {
	return s.bootstrapReconciliation.Reconcile(state, s.bootstrapState, s.catalog.Digest, s.now().UTC())
}

func (s *Service) createInitialState(store Store, state State) error {
	state.Generation = 1
	state.Version = stateVersion
	state.Catalog = s.catalog
	state.ProviderProfile = s.providerProfile
	state.Domains = append([]authorizationv1.PolicyDomain(nil), s.domains...)
	_, err := store.CompareAndSwap(0, state)
	return err
}
