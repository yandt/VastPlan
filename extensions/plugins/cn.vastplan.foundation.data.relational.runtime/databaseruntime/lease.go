package databaseruntime

import (
	"context"
	"errors"
	"sync"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

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
