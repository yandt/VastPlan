package authorizationpolicy

import (
	"fmt"
	"reflect"
	"time"
)

// BootstrapReconciliationPolicy is selected once by the trusted composition
// root. Runtime workflows never branch on environment or deployment mode.
type BootstrapReconciliationPolicy interface {
	Reconcile(current *State, bootstrap *State, targetCatalogDigest string, now time.Time) (bool, error)
	AllowSnapshotRecovery() bool
}

type DisabledBootstrapReconciliation struct{}

func (DisabledBootstrapReconciliation) Reconcile(_ *State, _ *State, _ string, _ time.Time) (bool, error) {
	return false, nil
}
func (DisabledBootstrapReconciliation) AllowSnapshotRecovery() bool { return false }

// SeedOwnedBootstrapReconciliation synchronizes only objects owned by the
// trusted Seed Authority. User-managed roles and bindings are never rewritten.
type SeedOwnedBootstrapReconciliation struct{}

func (SeedOwnedBootstrapReconciliation) AllowSnapshotRecovery() bool { return true }

func (SeedOwnedBootstrapReconciliation) Reconcile(current *State, bootstrap *State, targetCatalogDigest string, now time.Time) (bool, error) {
	if current == nil {
		return false, fmt.Errorf("Authorization 当前状态缺失")
	}
	if bootstrap == nil || bootstrap.Generation == 0 {
		return false, fmt.Errorf("显式 Seed 协调缺少 Bootstrap 状态")
	}
	if current.Version != stateVersion || bootstrap.Version != stateVersion {
		return false, fmt.Errorf("Authorization Bootstrap 状态版本无效")
	}
	if bootstrap.Catalog.Digest == "" || bootstrap.Catalog.Digest != targetCatalogDigest {
		return false, fmt.Errorf("Authorization Bootstrap Catalog 与运行目录不一致")
	}
	roles, err := reconcileSeedRoles(current.Roles, bootstrap.Roles, now.UTC())
	if err != nil {
		return false, err
	}
	bindings, err := reconcileSeedBindings(current.Bindings, bootstrap.Bindings, now.UTC())
	if err != nil {
		return false, err
	}
	if err := validateReconciledBindingRoles(roles, bindings); err != nil {
		return false, err
	}
	changed := !reflect.DeepEqual(current.Roles, roles) || !reflect.DeepEqual(current.Bindings, bindings)
	if changed {
		current.Roles = roles
		current.Bindings = bindings
	}
	return changed, nil
}

func validateReconciledBindingRoles(roles []RoleRevision, bindings []BindingRevision) error {
	known := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		known[fmt.Sprintf("%s\x00%d", role.ID, role.Revision)] = struct{}{}
	}
	for _, binding := range bindings {
		if _, exists := known[fmt.Sprintf("%s\x00%d", binding.RoleID, binding.RoleRevision)]; !exists {
			return fmt.Errorf("Binding %s 引用的 Role revision 不存在", binding.ID)
		}
	}
	return nil
}

func reconcileSeedRoles(current, expected []RoleRevision, now time.Time) ([]RoleRevision, error) {
	seed := make(map[string]RoleRevision)
	for _, role := range expected {
		if role.CreatedBy != "seed-authority" {
			continue
		}
		if role.ID == "" || role.Revision != 1 || role.State != StatePublished || role.ApprovedBy != "trusted-host" {
			return nil, fmt.Errorf("Seed Role %s 不满足受信基线约束", role.ID)
		}
		if _, duplicate := seed[role.ID]; duplicate {
			return nil, fmt.Errorf("Seed Role %s 重复", role.ID)
		}
		seed[role.ID] = role
	}
	result := make([]RoleRevision, 0, len(current)+len(seed))
	for _, role := range current {
		expectedRole, managed := seed[role.ID]
		if role.CreatedBy != "seed-authority" {
			if managed {
				return nil, fmt.Errorf("开发授权基线 Role %s 已被非受信定义占用", role.ID)
			}
			result = append(result, role)
			continue
		}
		if !managed {
			continue
		}
		if role.Revision != 1 {
			return nil, fmt.Errorf("开发授权基线 Role %s revision 非法", role.ID)
		}
		expectedRole.CreatedAt = role.CreatedAt
		expectedRole.UpdatedAt = role.UpdatedAt
		if !reflect.DeepEqual(role, expectedRole) {
			expectedRole.UpdatedAt = now
		}
		result = append(result, expectedRole)
		delete(seed, role.ID)
	}
	for _, role := range expected {
		if replacement, missing := seed[role.ID]; missing {
			result = append(result, replacement)
			delete(seed, role.ID)
		}
	}
	return result, nil
}

func reconcileSeedBindings(current, expected []BindingRevision, now time.Time) ([]BindingRevision, error) {
	seed := make(map[string]BindingRevision)
	for _, binding := range expected {
		if binding.CreatedBy != "seed-authority" {
			continue
		}
		if binding.ID == "" || binding.Revision != 1 || binding.State != StatePublished || binding.ApprovedBy != "trusted-host" {
			return nil, fmt.Errorf("Seed Binding %s 不满足受信基线约束", binding.ID)
		}
		if _, duplicate := seed[binding.ID]; duplicate {
			return nil, fmt.Errorf("Seed Binding %s 重复", binding.ID)
		}
		seed[binding.ID] = binding
	}
	result := make([]BindingRevision, 0, len(current)+len(seed))
	for _, binding := range current {
		expectedBinding, managed := seed[binding.ID]
		if binding.CreatedBy != "seed-authority" {
			if managed {
				return nil, fmt.Errorf("开发授权基线 Binding %s 已被非受信定义占用", binding.ID)
			}
			result = append(result, binding)
			continue
		}
		if !managed {
			continue
		}
		if binding.Revision != 1 {
			return nil, fmt.Errorf("开发授权基线 Binding %s revision 非法", binding.ID)
		}
		expectedBinding.CreatedAt = binding.CreatedAt
		expectedBinding.UpdatedAt = binding.UpdatedAt
		if !reflect.DeepEqual(binding, expectedBinding) {
			expectedBinding.UpdatedAt = now
		}
		result = append(result, expectedBinding)
		delete(seed, binding.ID)
	}
	for _, binding := range expected {
		if replacement, missing := seed[binding.ID]; missing {
			result = append(result, replacement)
			delete(seed, binding.ID)
		}
	}
	return result, nil
}
