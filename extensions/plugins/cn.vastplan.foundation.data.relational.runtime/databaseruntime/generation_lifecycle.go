package databaseruntime

import (
	"context"
	"errors"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func (m *PoolManager) Activate(ctx context.Context, scope RequestScope, spec databasev1.ConnectionSpec,
	material MaterialSource) (databasev1.ActivateResult, error) {
	if m == nil || ctx == nil || nilInterface(material) {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("activate 参数无效"))
	}
	if err := scope.validate(false); err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	if err := databasev1.ValidateConnectionSpec(spec); err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	fingerprint, err := poolFingerprint(scope, spec)
	if err != nil {
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	logical := connectionFor(scope, spec.Ref)

	m.activationMu.Lock()
	defer m.activationMu.Unlock()
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, errors.New("Pool Manager 已关闭"))
	}
	group := m.groups[logical]
	if group != nil && group.active != nil {
		active := group.active
		if active.fingerprint == fingerprint {
			m.counters.idempotent.Add(1)
			result := databasev1.ActivateResult{Connection: spec.Ref, Generation: active.generation, Ready: true}
			m.mu.Unlock()
			return result, nil
		}
		if spec.Ref.Revision <= active.spec.Ref.Revision {
			m.mu.Unlock()
			return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false,
				fmt.Errorf("连接 revision 必须递增，当前=%d 候选=%d", active.spec.Ref.Revision, spec.Ref.Revision))
		}
	}
	if group != nil && group.active == nil {
		var latestRevision uint64
		for _, generation := range group.generations {
			if generation.spec.Ref.Revision > latestRevision {
				latestRevision = generation.spec.Ref.Revision
			}
		}
		if spec.Ref.Revision < latestRevision {
			m.mu.Unlock()
			return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false,
				fmt.Errorf("连接 revision 不得回退，最近=%d 候选=%d", latestRevision, spec.Ref.Revision))
		}
	}
	if err := m.checkBudgetLocked(logical, spec.Pool.MaxOpen); err != nil {
		m.counters.budgetRejected.Add(1)
		m.mu.Unlock()
		return databasev1.ActivateResult{}, err
	}
	m.mu.Unlock()

	pool, err := m.registry.OpenPool(ctx, spec, material)
	if err != nil {
		return databasev1.ActivateResult{}, err
	}
	if err := pool.Probe(ctx); err != nil {
		_ = pool.Close()
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, err)
	}
	if err := ctx.Err(); err != nil {
		_ = pool.Close()
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorDeadlineExceeded, true, err)
	}

	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		_ = pool.Close()
		return databasev1.ActivateResult{}, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, errors.New("Pool Manager 已关闭"))
	}
	group = m.groups[logical]
	if group == nil {
		group = &connectionGroup{generations: map[uint64]*poolGeneration{}}
		m.groups[logical] = group
	}
	m.nextGeneration++
	generation := newPoolGeneration(logical, m.nextGeneration, fingerprint, spec, pool, m.policy.MaxWaitersPerPool)
	old := group.active
	group.generations[generation.generation] = generation
	group.active = generation
	if old != nil {
		old.markDraining()
	}
	m.counters.activations.Add(1)
	m.mu.Unlock()
	if old != nil {
		m.scheduleDrain(old)
	}
	return databasev1.ActivateResult{Connection: spec.Ref, Generation: generation.generation, Ready: true}, nil
}

func (m *PoolManager) Retire(ctx context.Context, scope RequestScope, ref databasev1.ConnectionRef) error {
	if m == nil || ctx == nil || scope.validate(false) != nil || databasev1.ValidateConnectionRef(ref) != nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("retire 参数无效"))
	}
	if err := ctx.Err(); err != nil {
		return NewRuntimeError(databasev1.ErrorDeadlineExceeded, true, err)
	}
	m.activationMu.Lock()
	defer m.activationMu.Unlock()
	logical := connectionFor(scope, ref)
	m.mu.Lock()
	group := m.groups[logical]
	if group == nil || group.active == nil || group.active.spec.Ref != ref {
		if group != nil {
			for _, generation := range group.generations {
				state, _, _, _ := generation.view()
				if generation.spec.Ref == ref && (state == PoolDraining || state == PoolClosed) {
					m.mu.Unlock()
					return nil
				}
			}
		}
		m.mu.Unlock()
		return NewRuntimeError(databasev1.ErrorConnectionNotFound, false, errors.New("活动连接 revision 不存在"))
	}
	entry := group.active
	group.active = nil
	entry.markDraining()
	m.counters.retirements.Add(1)
	m.mu.Unlock()
	m.scheduleDrain(entry)
	return nil
}

// RetireAll removes the same tenant-scoped connection revision from every
// project-local pool in this Runtime replica. Management deletion cannot know
// which projects have lazily hydrated a pool, so exact-project retirement
// would leave stale pools usable until process restart.
func (m *PoolManager) RetireAll(ctx context.Context, tenantID string, ref databasev1.ConnectionRef) error {
	if m == nil || ctx == nil || invalidScopePart(tenantID, true) || databasev1.ValidateConnectionRef(ref) != nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("retire all 参数无效"))
	}
	if err := ctx.Err(); err != nil {
		return NewRuntimeError(databasev1.ErrorDeadlineExceeded, true, err)
	}
	m.activationMu.Lock()
	defer m.activationMu.Unlock()
	m.mu.Lock()
	entries := make([]*poolGeneration, 0)
	for logical, group := range m.groups {
		if logical.tenant != tenantID || logical.resource != ref.ResourceID || group.active == nil || group.active.spec.Ref != ref {
			continue
		}
		entry := group.active
		group.active = nil
		entry.markDraining()
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		m.mu.Unlock()
		return nil
	}
	m.counters.retirements.Add(uint64(len(entries)))
	m.mu.Unlock()
	for _, entry := range entries {
		m.scheduleDrain(entry)
	}
	return nil
}
