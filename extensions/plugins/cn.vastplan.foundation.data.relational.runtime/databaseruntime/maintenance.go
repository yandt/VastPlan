package databaseruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

func (m *PoolManager) scheduleDrain(entry *poolGeneration) {
	go func() {
		timer := time.NewTimer(m.policy.DrainTimeout)
		defer timer.Stop()
		select {
		case <-entry.drained:
		case <-timer.C:
			m.counters.forcedDrains.Add(1)
		}
		m.closeEntry(entry)
	}()
}

func (m *PoolManager) closeEntry(entry *poolGeneration) {
	entry.mu.Lock()
	if entry.state == PoolClosed || entry.closing {
		entry.mu.Unlock()
		return
	}
	entry.closing = true
	entry.mu.Unlock()
	err := entry.pool.Close()
	entry.mu.Lock()
	entry.closing = false
	if err != nil {
		entry.closeFailed = true
		entry.mu.Unlock()
		m.counters.closeFailures.Add(1)
		return
	}
	entry.state = PoolClosed
	entry.closeFailed = false
	entry.closedOnce.Do(func() { close(entry.closed) })
	entry.mu.Unlock()
	m.mu.Lock()
	m.pruneClosedLocked()
	m.mu.Unlock()
}

func (m *PoolManager) checkBudgetLocked(logical logicalConnection, requested int) error {
	node, tenant, connection, generations := 0, 0, 0, 0
	for key, group := range m.groups {
		for _, entry := range group.generations {
			state, _, _, _ := entry.view()
			if state == PoolClosed {
				continue
			}
			reserved := entry.spec.Pool.MaxOpen
			node += reserved
			if key.tenant == logical.tenant {
				tenant += reserved
			}
			if key == logical {
				connection += reserved
				generations++
			}
		}
	}
	if node+requested > m.policy.NodeMaxOpen || tenant+requested > m.policy.TenantMaxOpen ||
		connection+requested > m.policy.ConnectionMaxOpen || generations+1 > m.policy.MaxGenerations {
		return NewRuntimeError(databasev1.ErrorPoolExhausted, true, fmt.Errorf(
			"连接预算不足 node=%d/%d tenant=%d/%d connection=%d/%d generations=%d/%d request=%d",
			node, m.policy.NodeMaxOpen, tenant, m.policy.TenantMaxOpen,
			connection, m.policy.ConnectionMaxOpen, generations, m.policy.MaxGenerations, requested))
	}
	return nil
}

func (m *PoolManager) pruneClosedLocked() {
	type closedGeneration struct {
		logical    logicalConnection
		generation uint64
	}
	closed := make([]closedGeneration, 0)
	for logical, group := range m.groups {
		for generation, entry := range group.generations {
			state, _, _, _ := entry.view()
			if state == PoolClosed {
				closed = append(closed, closedGeneration{logical: logical, generation: generation})
			}
		}
	}
	sort.Slice(closed, func(i, j int) bool { return closed[i].generation < closed[j].generation })
	for len(closed) > m.policy.ClosedHistoryLimit {
		candidate := closed[0]
		group := m.groups[candidate.logical]
		delete(group.generations, candidate.generation)
		if group.active == nil && len(group.generations) == 0 {
			delete(m.groups, candidate.logical)
		}
		closed = closed[1:]
	}
}

func (m *PoolManager) Close(ctx context.Context) error {
	if m == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("Pool Manager close context 不能为空")
	}
	m.activationMu.Lock()
	m.mu.Lock()
	m.closed = true
	entries := make([]*poolGeneration, 0)
	for _, group := range m.groups {
		group.active = nil
		for _, entry := range group.generations {
			state, _, _, _ := entry.view()
			if state != PoolClosed {
				entry.markDraining()
				entries = append(entries, entry)
			}
		}
	}
	m.mu.Unlock()
	m.activationMu.Unlock()
	for _, entry := range entries {
		m.scheduleDrain(entry)
	}
	var failures []error
	for _, entry := range entries {
		select {
		case <-entry.closed:
		case <-ctx.Done():
			failures = append(failures, ctx.Err())
			return errors.Join(failures...)
		}
	}
	return errors.Join(failures...)
}

type GenerationSnapshot struct {
	ScopeHash      string    `json:"scopeHash"`
	ConnectionHash string    `json:"connectionHash"`
	Revision       uint64    `json:"revision"`
	ProviderID     string    `json:"providerId"`
	Generation     uint64    `json:"generation"`
	State          PoolState `json:"state"`
	MaxOpen        int       `json:"maxOpen"`
	InFlight       int       `json:"inFlight"`
	Waiting        int       `json:"waiting"`
	CloseFailed    bool      `json:"closeFailed"`
	Pool           PoolStats `json:"pool"`
}

type ManagerSnapshot struct {
	NodeReserved          int                  `json:"nodeReserved"`
	TenantCount           int                  `json:"tenantCount"`
	ConnectionCount       int                  `json:"connectionCount"`
	Activations           uint64               `json:"activations"`
	IdempotentActivations uint64               `json:"idempotentActivations"`
	Retirements           uint64               `json:"retirements"`
	BudgetRejected        uint64               `json:"budgetRejected"`
	AcquireSucceeded      uint64               `json:"acquireSucceeded"`
	AcquireWaitMS         uint64               `json:"acquireWaitMs"`
	AcquireTimeouts       uint64               `json:"acquireTimeouts"`
	QueueRejected         uint64               `json:"queueRejected"`
	ForcedDrains          uint64               `json:"forcedDrains"`
	CloseFailures         uint64               `json:"closeFailures"`
	Generations           []GenerationSnapshot `json:"generations"`
}

func (m *PoolManager) Snapshot() ManagerSnapshot {
	if m == nil {
		return ManagerSnapshot{Generations: []GenerationSnapshot{}}
	}
	snapshot := ManagerSnapshot{
		Activations: m.counters.activations.Load(), IdempotentActivations: m.counters.idempotent.Load(),
		Retirements: m.counters.retirements.Load(), BudgetRejected: m.counters.budgetRejected.Load(),
		AcquireSucceeded: m.counters.acquireSucceeded.Load(),
		AcquireWaitMS:    m.counters.acquireWaitNanos.Load() / uint64(time.Millisecond),
		AcquireTimeouts:  m.counters.acquireTimeouts.Load(), QueueRejected: m.counters.queueRejected.Load(),
		ForcedDrains: m.counters.forcedDrains.Load(), CloseFailures: m.counters.closeFailures.Load(),
		Generations: []GenerationSnapshot{},
	}
	type snapshotEntry struct {
		logical logicalConnection
		entry   *poolGeneration
	}
	entries := make([]snapshotEntry, 0)
	m.mu.RLock()
	for logical, group := range m.groups {
		for _, entry := range group.generations {
			entries = append(entries, snapshotEntry{logical: logical, entry: entry})
		}
	}
	m.mu.RUnlock()
	tenants, connections := map[string]struct{}{}, map[string]struct{}{}
	for _, candidate := range entries {
		logical, entry := candidate.logical, candidate.entry
		state, inflight, waiting, closeFailed := entry.view()
		if state != PoolClosed {
			snapshot.NodeReserved += entry.spec.Pool.MaxOpen
			tenants[logical.tenant] = struct{}{}
			connections[logicalKey(logical)] = struct{}{}
		}
		snapshot.Generations = append(snapshot.Generations, GenerationSnapshot{
			ScopeHash: shortDigest(logical.tenant + "\x00" + logical.project), ConnectionHash: shortDigest(logicalKey(logical)),
			Revision: entry.spec.Ref.Revision, ProviderID: entry.spec.ProviderID, Generation: entry.generation,
			State: state, MaxOpen: entry.spec.Pool.MaxOpen, InFlight: inflight, Waiting: waiting,
			CloseFailed: closeFailed, Pool: entry.pool.Stats(),
		})
	}
	snapshot.TenantCount, snapshot.ConnectionCount = len(tenants), len(connections)
	sort.Slice(snapshot.Generations, func(i, j int) bool { return snapshot.Generations[i].Generation < snapshot.Generations[j].Generation })
	return snapshot
}

func poolFingerprint(scope RequestScope, spec databasev1.ConnectionSpec) (string, error) {
	canonical := struct {
		Tenant            string                   `json:"tenant"`
		Project           string                   `json:"project"`
		Ref               databasev1.ConnectionRef `json:"ref"`
		Provider          string                   `json:"provider"`
		Endpoint          string                   `json:"endpoint"`
		Database          string                   `json:"database"`
		Options           json.RawMessage          `json:"options"`
		CredentialHandle  string                   `json:"credentialHandle"`
		CredentialVersion int64                    `json:"credentialVersion"`
		Pool              databasev1.PoolPolicy    `json:"pool"`
	}{scope.TenantID, scope.ProjectID, spec.Ref, spec.ProviderID, spec.Endpoint, spec.Database,
		spec.Options, spec.Credentials.Handle, spec.Credentials.Version, spec.Pool}
	raw, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func logicalKey(logical logicalConnection) string {
	return logical.tenant + "\x00" + logical.project + "\x00" + logical.resource
}
func shortDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:8])
}
