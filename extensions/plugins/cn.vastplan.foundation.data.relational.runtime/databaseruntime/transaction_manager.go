package databaseruntime

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

const (
	transactionHandlePrefix = "vptx1"
	defaultMaxTransactions  = 4096
)

type transactionClaims struct {
	ID         string                   `json:"id"`
	Tenant     string                   `json:"tenant"`
	Project    string                   `json:"project,omitempty"`
	CallerKind contractv1.CallerKind    `json:"callerKind"`
	CallerID   string                   `json:"callerId"`
	Connection databasev1.ConnectionRef `json:"connection"`
	ExpiresAt  int64                    `json:"expiresAt"`
}

type activeTransaction struct {
	claims      transactionClaims
	transaction Transaction
	lease       *PoolLease
	timer       *time.Timer
	done        chan struct{}
	doneOnce    sync.Once
	mu          sync.Mutex
}

type TransactionManager struct {
	instanceID string
	aead       cipher.AEAD
	maxActive  int
	mu         sync.Mutex
	active     map[string]*activeTransaction
	closed     bool
	counters   transactionCounters
}

type transactionCounters struct {
	begins    atomic.Uint64
	commits   atomic.Uint64
	rollbacks atomic.Uint64
	expired   atomic.Uint64
	lost      atomic.Uint64
	rejected  atomic.Uint64
}

// TransactionSnapshot contains counters and capacity only. It deliberately
// excludes handles, callers, connection refs and owner routes.
type TransactionSnapshot struct {
	Active    uint64
	Capacity  uint64
	Begins    uint64
	Commits   uint64
	Rollbacks uint64
	Expired   uint64
	Lost      uint64
	Rejected  uint64
}

func NewTransactionManager(instanceID string, maxActive int) (*TransactionManager, error) {
	if strings.TrimSpace(instanceID) == "" || instanceID != strings.TrimSpace(instanceID) || len(instanceID) > 512 {
		return nil, errors.New("Database Runtime instance ID 无效")
	}
	if maxActive == 0 {
		maxActive = defaultMaxTransactions
	}
	if maxActive < 1 || maxActive > 1_000_000 {
		return nil, errors.New("Database Runtime transaction 上限无效")
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("生成事务句柄密钥: %w", err)
	}
	block, err := aes.NewCipher(key)
	for index := range key {
		key[index] = 0
	}
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &TransactionManager{instanceID: instanceID, aead: aead, maxActive: maxActive, active: map[string]*activeTransaction{}}, nil
}

func TransactionRoute(handle string) (string, error) {
	parts := strings.Split(handle, ".")
	if len(parts) != 3 || parts[0] != transactionHandlePrefix {
		return "", errors.New("事务句柄格式无效")
	}
	route, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(route) == 0 || len(route) > 512 || base64.RawURLEncoding.EncodeToString(route) != parts[1] {
		return "", errors.New("事务句柄路由无效")
	}
	if len(parts[2]) < 32 || len(parts[2]) > 1536 {
		return "", errors.New("事务句柄密文无效")
	}
	return string(route), nil
}

func (m *TransactionManager) seal(claims transactionClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, m.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	route := base64.RawURLEncoding.EncodeToString([]byte(m.instanceID))
	sealed := m.aead.Seal(nonce, nonce, raw, []byte(transactionHandlePrefix+"."+route))
	return transactionHandlePrefix + "." + route + "." + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (m *TransactionManager) open(handle string) (transactionClaims, error) {
	route, err := TransactionRoute(handle)
	if err != nil {
		return transactionClaims{}, err
	}
	if route != m.instanceID {
		return transactionClaims{}, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务所属 Runtime 实例不可用"))
	}
	parts := strings.Split(handle, ".")
	sealed, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(sealed) <= m.aead.NonceSize() {
		return transactionClaims{}, errors.New("事务句柄密文无效")
	}
	nonce, ciphertext := sealed[:m.aead.NonceSize()], sealed[m.aead.NonceSize():]
	raw, err := m.aead.Open(nil, nonce, ciphertext, []byte(parts[0]+"."+parts[1]))
	if err != nil {
		return transactionClaims{}, errors.New("事务句柄签名无效")
	}
	var claims transactionClaims
	if err := json.Unmarshal(raw, &claims); err != nil || claims.ID == "" || claims.ExpiresAt <= 0 || databasev1.ValidateConnectionRef(claims.Connection) != nil {
		return transactionClaims{}, errors.New("事务句柄声明无效")
	}
	return claims, nil
}

func randomTransactionID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func claimsFor(call *contractv1.CallContext, ref databasev1.ConnectionRef, expires time.Time) (transactionClaims, error) {
	if call == nil || call.GetCaller().GetId() == "" {
		return transactionClaims{}, errors.New("事务调用缺少可信 caller")
	}
	id, err := randomTransactionID()
	if err != nil {
		return transactionClaims{}, err
	}
	return transactionClaims{ID: id, Tenant: call.GetTenantId(), Project: call.GetProjectId(), CallerKind: call.GetCaller().GetKind(), CallerID: call.GetCaller().GetId(), Connection: ref, ExpiresAt: expires.UnixMilli()}, nil
}

func (m *TransactionManager) Begin(ctx context.Context, call *contractv1.CallContext, ref databasev1.ConnectionRef,
	options databasev1.TransactionOptions, lease *PoolLease) (databasev1.BeginResult, error) {
	if m == nil || lease == nil {
		return databasev1.BeginResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("begin transaction 参数无效"))
	}
	m.mu.Lock()
	if m.closed || len(m.active) >= m.maxActive {
		m.mu.Unlock()
		m.counters.rejected.Add(1)
		return databasev1.BeginResult{}, NewRuntimeError(databasev1.ErrorPoolExhausted, true, errors.New("Runtime 活动事务达到上限"))
	}
	m.mu.Unlock()
	expires := time.Now().Add(time.Duration(options.TimeoutMS) * time.Millisecond)
	claims, err := claimsFor(call, ref, expires)
	if err != nil {
		return databasev1.BeginResult{}, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	transaction, err := lease.Begin(ctx, options)
	if err != nil {
		return databasev1.BeginResult{}, err
	}
	entry := &activeTransaction{claims: claims, transaction: transaction, lease: lease, done: make(chan struct{})}
	handle, err := m.seal(claims)
	if err != nil {
		_ = transaction.Rollback(context.Background())
		return databasev1.BeginResult{}, err
	}
	m.mu.Lock()
	if m.closed || len(m.active) >= m.maxActive {
		m.mu.Unlock()
		_ = transaction.Rollback(context.Background())
		m.counters.rejected.Add(1)
		return databasev1.BeginResult{}, NewRuntimeError(databasev1.ErrorPoolExhausted, true, errors.New("Runtime 活动事务达到上限"))
	}
	m.active[claims.ID] = entry
	m.counters.begins.Add(1)
	entry.timer = time.AfterFunc(time.Until(expires), func() { m.expire(claims.ID) })
	m.mu.Unlock()
	go func() {
		select {
		case <-lease.Closed():
			m.lose(claims.ID)
		case <-entry.done:
		}
	}()
	return databasev1.BeginResult{TransactionHandle: handle, ExpiresAt: expires.UTC()}, nil
}

func validateTransactionCaller(claims transactionClaims, call *contractv1.CallContext) error {
	if call == nil || call.GetTenantId() != claims.Tenant || call.GetProjectId() != claims.Project ||
		call.GetCaller().GetKind() != claims.CallerKind || call.GetCaller().GetId() != claims.CallerID {
		return NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务句柄不属于当前调用方或 scope"))
	}
	return nil
}

func (m *TransactionManager) resolve(handle string, call *contractv1.CallContext, ref *databasev1.ConnectionRef) (*activeTransaction, transactionClaims, error) {
	claims, err := m.open(handle)
	if err != nil {
		var runtimeErr *RuntimeError
		if errors.As(err, &runtimeErr) {
			return nil, claims, err
		}
		return nil, claims, NewRuntimeError(databasev1.ErrorInvalidRequest, false, err)
	}
	if err := validateTransactionCaller(claims, call); err != nil {
		return nil, claims, err
	}
	if ref != nil && *ref != claims.Connection {
		return nil, claims, NewRuntimeError(databasev1.ErrorInvalidRequest, false, errors.New("事务连接 revision 不匹配"))
	}
	if time.Now().UnixMilli() >= claims.ExpiresAt {
		m.expire(claims.ID)
		return nil, claims, NewRuntimeError(databasev1.ErrorTransactionExpired, false, errors.New("事务已过期并回滚"))
	}
	m.mu.Lock()
	entry := m.active[claims.ID]
	m.mu.Unlock()
	if entry == nil {
		return nil, claims, NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务状态已丢失"))
	}
	return entry, claims, nil
}

func (m *TransactionManager) Connection(handle string, call *contractv1.CallContext) (databasev1.ConnectionRef, error) {
	_, claims, err := m.resolve(handle, call, nil)
	if err != nil {
		return databasev1.ConnectionRef{}, err
	}
	return claims.Connection, nil
}

func (m *TransactionManager) Query(ctx context.Context, call *contractv1.CallContext, request *databasev1.QueryRequest) (databasev1.QueryResult, error) {
	entry, claims, err := m.resolve(request.TransactionHandle, call, &request.Connection)
	if err != nil {
		return databasev1.QueryResult{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().UnixMilli() >= claims.ExpiresAt {
		return databasev1.QueryResult{}, NewRuntimeError(databasev1.ErrorTransactionExpired, false, errors.New("事务已过期"))
	}
	return entry.transaction.Query(ctx, request.Statement, request.MaxRows)
}

func (m *TransactionManager) Execute(ctx context.Context, call *contractv1.CallContext, request *databasev1.ExecuteRequest) (databasev1.ExecuteResult, error) {
	entry, claims, err := m.resolve(request.TransactionHandle, call, &request.Connection)
	if err != nil {
		return databasev1.ExecuteResult{}, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if time.Now().UnixMilli() >= claims.ExpiresAt {
		return databasev1.ExecuteResult{}, NewRuntimeError(databasev1.ErrorTransactionExpired, false, errors.New("事务已过期"))
	}
	return entry.transaction.Execute(ctx, request.Statement)
}

func (m *TransactionManager) End(ctx context.Context, call *contractv1.CallContext, handle string, commit bool) error {
	entry, claims, err := m.resolve(handle, call, nil)
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.active[claims.ID] != entry {
		m.mu.Unlock()
		return NewRuntimeError(databasev1.ErrorTransactionLost, true, errors.New("事务已结束"))
	}
	delete(m.active, claims.ID)
	m.mu.Unlock()
	entry.timer.Stop()
	entry.doneOnce.Do(func() { close(entry.done) })
	entry.mu.Lock()
	defer entry.mu.Unlock()
	defer entry.lease.Release()
	if commit {
		err := entry.transaction.Commit(ctx)
		if err == nil {
			m.counters.commits.Add(1)
		}
		return err
	}
	err = entry.transaction.Rollback(ctx)
	if err == nil {
		m.counters.rollbacks.Add(1)
	}
	return err
}

func (m *TransactionManager) expire(id string) {
	if m.removeAndRollback(id) {
		m.counters.expired.Add(1)
	}
}

func (m *TransactionManager) lose(id string) {
	if m.removeAndRollback(id) {
		m.counters.lost.Add(1)
	}
}

func (m *TransactionManager) removeAndRollback(id string) bool {
	m.mu.Lock()
	entry := m.active[id]
	delete(m.active, id)
	m.mu.Unlock()
	if entry == nil {
		return false
	}
	if entry.timer != nil {
		entry.timer.Stop()
	}
	entry.doneOnce.Do(func() { close(entry.done) })
	entry.mu.Lock()
	_ = entry.transaction.Rollback(context.Background())
	entry.lease.Release()
	entry.mu.Unlock()
	return true
}

func (m *TransactionManager) Snapshot() TransactionSnapshot {
	if m == nil {
		return TransactionSnapshot{}
	}
	m.mu.Lock()
	active, capacity := len(m.active), m.maxActive
	m.mu.Unlock()
	return TransactionSnapshot{
		Active: uint64(active), Capacity: uint64(capacity),
		Begins: m.counters.begins.Load(), Commits: m.counters.commits.Load(), Rollbacks: m.counters.rollbacks.Load(),
		Expired: m.counters.expired.Load(), Lost: m.counters.lost.Load(), Rejected: m.counters.rejected.Load(),
	}
}

func (m *TransactionManager) Close() error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	m.closed = true
	ids := make([]string, 0, len(m.active))
	for id := range m.active {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.expire(id)
	}
	return nil
}
