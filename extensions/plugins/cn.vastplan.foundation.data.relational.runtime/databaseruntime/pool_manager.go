package databaseruntime

import (
	"errors"
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
