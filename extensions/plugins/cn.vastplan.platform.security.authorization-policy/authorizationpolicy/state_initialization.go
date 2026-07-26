package authorizationpolicy

import (
	"bytes"
	"encoding/json"
	"fmt"

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
		return s.createInitialState(store, state)
	}
	if state.Catalog.Digest == s.catalog.Digest && bytes.Equal(mustJSON(state.ProviderProfile), mustJSON(s.providerProfile)) && bytes.Equal(mustJSON(state.Domains), mustJSON(s.domains)) {
		return nil
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
	previousGeneration := state.Generation
	state.Catalog = s.catalog
	state.ProviderProfile = s.providerProfile
	state.Domains = append([]authorizationv1.PolicyDomain(nil), s.domains...)
	state.Generation++
	state.Audit = append(state.Audit, AuditEvent{ID: randomID("audit"), Action: "catalogUpdate", ObjectKind: "catalog", ObjectID: s.catalog.Digest, Revision: state.Generation, SubjectID: "trusted-host", OccurredAt: s.now().UTC()})
	_, err = store.CompareAndSwap(previousGeneration, state)
	return err
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
