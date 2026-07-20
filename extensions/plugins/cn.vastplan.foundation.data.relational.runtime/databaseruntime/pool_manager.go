package databaseruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

type PoolState string

const (
	PoolReady    PoolState = "ready"
	PoolDraining PoolState = "draining"
	PoolClosed   PoolState = "closed"
)

type ManagerPolicy struct {
	NodeMaxOpen            int
	TenantMaxOpen          int
	ConnectionMaxOpen      int
	MaxGenerations         int
	MaxWaitersPerPool      int
	MaxConcurrentPerCaller int
	DrainTimeout           time.Duration
	ClosedHistoryLimit     int
}

func DefaultManagerPolicy() ManagerPolicy {
	return ManagerPolicy{
		NodeMaxOpen: 4096, TenantMaxOpen: 1024, ConnectionMaxOpen: 256,
		MaxGenerations: 2, MaxWaitersPerPool: 1024, MaxConcurrentPerCaller: 128,
		DrainTimeout: 30 * time.Second, ClosedHistoryLimit: 8,
	}
}

func (p ManagerPolicy) normalize() (ManagerPolicy, error) {
	defaults := DefaultManagerPolicy()
	if p.NodeMaxOpen == 0 {
		p.NodeMaxOpen = defaults.NodeMaxOpen
	}
	if p.TenantMaxOpen == 0 {
		p.TenantMaxOpen = defaults.TenantMaxOpen
	}
	if p.ConnectionMaxOpen == 0 {
		p.ConnectionMaxOpen = defaults.ConnectionMaxOpen
	}
	if p.MaxGenerations == 0 {
		p.MaxGenerations = defaults.MaxGenerations
	}
	if p.MaxWaitersPerPool == 0 {
		p.MaxWaitersPerPool = defaults.MaxWaitersPerPool
	}
	if p.MaxConcurrentPerCaller == 0 {
		p.MaxConcurrentPerCaller = defaults.MaxConcurrentPerCaller
	}
	if p.DrainTimeout == 0 {
		p.DrainTimeout = defaults.DrainTimeout
	}
	if p.ClosedHistoryLimit == 0 {
		p.ClosedHistoryLimit = defaults.ClosedHistoryLimit
	}
	if p.NodeMaxOpen < 1 || p.TenantMaxOpen < 1 || p.ConnectionMaxOpen < 1 || p.MaxGenerations < 1 ||
		p.MaxWaitersPerPool < 1 || p.MaxConcurrentPerCaller < 1 || p.DrainTimeout < 0 || p.ClosedHistoryLimit < 1 {
		return ManagerPolicy{}, errors.New("Database Pool Manager policy 无效")
	}
	if p.TenantMaxOpen > p.NodeMaxOpen || p.ConnectionMaxOpen > p.TenantMaxOpen {
		return ManagerPolicy{}, errors.New("连接预算必须满足 connection <= tenant <= node")
	}
	return p, nil
}

type RequestScope struct {
	TenantID  string
	ProjectID string
	CallerID  string
}

func (s RequestScope) validate(requireCaller bool) error {
	if invalidScopePart(s.TenantID, true) || invalidScopePart(s.ProjectID, false) ||
		(requireCaller && invalidScopePart(s.CallerID, true)) {
		return errors.New("Database Runtime scope 无效")
	}
	return nil
}

func invalidScopePart(value string, required bool) bool {
	trimmed := strings.TrimSpace(value)
	return (required && trimmed == "") || value != trimmed || len(value) > 256
}

type logicalConnection struct{ tenant, project, resource string }

func connectionFor(scope RequestScope, ref databasev1.ConnectionRef) logicalConnection {
	return logicalConnection{tenant: scope.TenantID, project: scope.ProjectID, resource: ref.ResourceID}
}

type connectionGroup struct {
	active      *poolGeneration
	generations map[uint64]*poolGeneration
}

type poolGeneration struct {
	logical     logicalConnection
	generation  uint64
	fingerprint string
	spec        databasev1.ConnectionSpec
	pool        Pool
	maxWaiters  int

	mu           sync.Mutex
	state        PoolState
	inflight     int
	waiting      int
	stateChanged chan struct{}
	drained      chan struct{}
	closed       chan struct{}
	drainOnce    sync.Once
	drainedOnce  sync.Once
	closedOnce   sync.Once
	closing      bool
	closeFailed  bool
	slots        chan struct{}
}

func newPoolGeneration(logical logicalConnection, generation uint64, fingerprint string,
	spec databasev1.ConnectionSpec, pool Pool, maxWaiters int) *poolGeneration {
	return &poolGeneration{
		logical: logical, generation: generation, fingerprint: fingerprint, spec: spec, pool: pool,
		maxWaiters: maxWaiters, state: PoolReady, stateChanged: make(chan struct{}),
		drained: make(chan struct{}), closed: make(chan struct{}), slots: make(chan struct{}, spec.Pool.MaxOpen),
	}
}

func (g *poolGeneration) markDraining() bool {
	changed := false
	g.drainOnce.Do(func() {
		g.mu.Lock()
		if g.state == PoolReady {
			g.state = PoolDraining
			close(g.stateChanged)
			changed = true
		}
		if g.inflight == 0 {
			g.drainedOnce.Do(func() { close(g.drained) })
		}
		g.mu.Unlock()
	})
	return changed
}

var errGenerationChanged = errors.New("database pool generation changed")

func (g *poolGeneration) beginWait() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.state != PoolReady {
		return errGenerationChanged
	}
	if g.waiting >= g.maxWaiters {
		return NewRuntimeError(databasev1.ErrorPoolExhausted, true, errors.New("连接池等待队列已满"))
	}
	g.waiting++
	return nil
}

func (g *poolGeneration) endWait(acquired bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.waiting > 0 {
		g.waiting--
	}
	if g.state != PoolReady {
		return errGenerationChanged
	}
	if acquired {
		g.inflight++
	}
	return nil
}

func (g *poolGeneration) releaseOperation() {
	<-g.slots
	g.mu.Lock()
	if g.inflight > 0 {
		g.inflight--
	}
	if g.state == PoolDraining && g.inflight == 0 {
		g.drainedOnce.Do(func() { close(g.drained) })
	}
	g.mu.Unlock()
}

func (g *poolGeneration) view() (PoolState, int, int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state, g.inflight, g.waiting, g.closeFailed
}

type callerGate struct {
	slots chan struct{}
	refs  int
}

type managerCounters struct {
	activations      atomic.Uint64
	idempotent       atomic.Uint64
	retirements      atomic.Uint64
	budgetRejected   atomic.Uint64
	acquireSucceeded atomic.Uint64
	acquireWaitNanos atomic.Uint64
	acquireTimeouts  atomic.Uint64
	queueRejected    atomic.Uint64
	forcedDrains     atomic.Uint64
	closeFailures    atomic.Uint64
}

type PoolManager struct {
	registry       *Registry
	policy         ManagerPolicy
	activationMu   sync.Mutex
	mu             sync.RWMutex
	groups         map[logicalConnection]*connectionGroup
	callerGates    map[string]*callerGate
	nextGeneration uint64
	closed         bool
	counters       managerCounters
}

func NewPoolManager(registry *Registry, policy ManagerPolicy) (*PoolManager, error) {
	if registry == nil {
		return nil, errors.New("Database Provider Registry 不能为空")
	}
	normalized, err := policy.normalize()
	if err != nil {
		return nil, err
	}
	return &PoolManager{
		registry: registry, policy: normalized, groups: map[logicalConnection]*connectionGroup{},
		callerGates: map[string]*callerGate{},
	}, nil
}

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

type PoolLease struct {
	entry   *poolGeneration
	manager *PoolManager
	gateKey string
	gate    *callerGate
	once    sync.Once
}

func (l *PoolLease) Generation() uint64 {
	if l == nil || l.entry == nil {
		return 0
	}
	return l.entry.generation
}

func (l *PoolLease) Closed() <-chan struct{} {
	if l == nil || l.entry == nil {
		closed := make(chan struct{})
		close(closed)
		return closed
	}
	return l.entry.closed
}

func (l *PoolLease) Probe(ctx context.Context) error {
	if l == nil || l.entry == nil || ctx == nil {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("pool lease probe 参数无效"))
	}
	return l.entry.pool.Probe(ctx)
}

func (l *PoolLease) Query(ctx context.Context, statement databasev1.Statement, maxRows int) (databasev1.QueryResult, error) {
	if l == nil || l.entry == nil || ctx == nil {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("pool lease query 参数无效"))
	}
	return l.entry.pool.Query(ctx, statement, maxRows)
}

func (l *PoolLease) Execute(ctx context.Context, statement databasev1.Statement) (databasev1.ExecuteResult, error) {
	if l == nil || l.entry == nil || ctx == nil {
		return databasev1.ExecuteResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("pool lease execute 参数无效"))
	}
	return l.entry.pool.Execute(ctx, statement)
}

func (l *PoolLease) Begin(ctx context.Context, options databasev1.TransactionOptions) (Transaction, error) {
	if l == nil || l.entry == nil || ctx == nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("pool lease begin 参数无效"))
	}
	return l.entry.pool.Begin(ctx, options)
}

func (l *PoolLease) Release() {
	if l == nil {
		return
	}
	l.once.Do(func() {
		l.entry.releaseOperation()
		<-l.gate.slots
		l.manager.releaseCallerGate(l.gateKey, l.gate)
	})
}

func (m *PoolManager) Acquire(ctx context.Context, scope RequestScope, ref databasev1.ConnectionRef) (*PoolLease, error) {
	if m == nil || ctx == nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("acquire 参数无效"))
	}
	if err := scope.validate(true); err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	if err := databasev1.ValidateConnectionRef(ref); err != nil {
		return nil, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		entry, err := m.resolveActive(scope, ref)
		if err != nil {
			return nil, err
		}
		lease, err := m.acquireEntry(ctx, scope, entry)
		if errors.Is(err, errGenerationChanged) {
			continue
		}
		return lease, err
	}
	return nil, NewRuntimeError(databasev1.ErrorConnectionUnavailable, true, errors.New("连接 generation 正在切换"))
}

func (m *PoolManager) resolveActive(scope RequestScope, ref databasev1.ConnectionRef) (*poolGeneration, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.closed {
		return nil, NewRuntimeError(databasev1.ErrorConnectionUnavailable, false, errors.New("Pool Manager 已关闭"))
	}
	group := m.groups[connectionFor(scope, ref)]
	if group == nil || group.active == nil || group.active.spec.Ref != ref {
		return nil, NewRuntimeError(databasev1.ErrorConnectionNotFound, false, errors.New("活动连接 revision 不存在"))
	}
	return group.active, nil
}

func (m *PoolManager) acquireEntry(ctx context.Context, scope RequestScope, entry *poolGeneration) (*PoolLease, error) {
	if err := entry.beginWait(); err != nil {
		if code, _ := ErrorDetails(err); code == databasev1.ErrorPoolExhausted {
			m.counters.queueRejected.Add(1)
		}
		return nil, err
	}
	waitStarted := time.Now()
	defer func() { m.counters.acquireWaitNanos.Add(uint64(time.Since(waitStarted))) }()
	waiting := true
	defer func() {
		if waiting {
			_ = entry.endWait(false)
		}
	}()
	timeout := time.Duration(entry.spec.Pool.AcquireTimeoutMS) * time.Millisecond
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	gateKey, gate := m.acquireCallerGate(scope)
	gateHeld := false
	defer func() {
		if !gateHeld {
			m.releaseCallerGate(gateKey, gate)
		}
	}()
	select {
	case gate.slots <- struct{}{}:
		gateHeld = true
	case <-entry.stateChanged:
		return nil, errGenerationChanged
	case <-waitCtx.Done():
		m.counters.acquireTimeouts.Add(1)
		return nil, NewRuntimeError(databasev1.ErrorPoolExhausted, true, errors.New("调用方并发等待超时"))
	}
	select {
	case entry.slots <- struct{}{}:
	case <-entry.stateChanged:
		<-gate.slots
		gateHeld = false
		return nil, errGenerationChanged
	case <-waitCtx.Done():
		<-gate.slots
		gateHeld = false
		m.counters.acquireTimeouts.Add(1)
		return nil, NewRuntimeError(databasev1.ErrorPoolExhausted, true, errors.New("连接池获取超时"))
	}
	if err := entry.endWait(true); err != nil {
		<-entry.slots
		<-gate.slots
		gateHeld = false
		waiting = false
		return nil, err
	}
	waiting = false
	m.counters.acquireSucceeded.Add(1)
	return &PoolLease{entry: entry, manager: m, gateKey: gateKey, gate: gate}, nil
}

func (m *PoolManager) acquireCallerGate(scope RequestScope) (string, *callerGate) {
	key := scope.TenantID + "\x00" + scope.CallerID
	m.mu.Lock()
	defer m.mu.Unlock()
	gate := m.callerGates[key]
	if gate == nil {
		gate = &callerGate{slots: make(chan struct{}, m.policy.MaxConcurrentPerCaller)}
		m.callerGates[key] = gate
	}
	gate.refs++
	return key, gate
}

func (m *PoolManager) releaseCallerGate(key string, gate *callerGate) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if current := m.callerGates[key]; current == gate {
		gate.refs--
		if gate.refs == 0 {
			delete(m.callerGates, key)
		}
	}
}

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
