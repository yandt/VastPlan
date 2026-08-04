package addressing

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

// RegisterOptions 描述一个可被本地直调和远端 queue group 调用的实例。
type RegisterOptions struct {
	Capability           string
	ExtensionPoint       string
	ServiceRole          string
	LogicalService       string
	RoutingDomain        string
	PartitionKey         string
	InstancePolicy       string
	StateModel           string
	Visibility           string
	Routing              string
	Readiness            string
	ReadinessReason      string
	Generation           uint64
	FencingToken         string
	LeaseExpiresAt       time.Time
	UnitID               string
	PluginID             string
	ArtifactVersion      string
	ArtifactSHA256       string
	ContractVersion      string
	InterfaceFingerprint string
	InstanceID           string
}

type Registration struct {
	router    *Router
	record    Announcement
	recordMu  sync.Mutex
	handler   InvokeHandler
	key       string
	id        string
	sub       *nats.Subscription
	directSub *nats.Subscription
	stream    bool
	localOnly bool
	cancel    context.CancelFunc
	active    atomic.Bool
	once      sync.Once
	closeErr  error
}

// PrepareLocalRegistration 创建只存在于当前 Router 的 local capability 候选。它不写
// 全局目录、不订阅 NATS，仍通过 ActivateRegistrations 参与候选组原子门闩。
func (r *Router) PrepareLocalRegistration(ctx context.Context, options RegisterOptions, handler InvokeHandler) (*Registration, error) {
	if options.Capability == "" || options.ExtensionPoint == "" || handler == nil {
		return nil, errors.New("local capability、extension point 和 handler 不能为空")
	}
	policy := servicemodel.Normalize(servicemodel.Policy{InstancePolicy: options.InstancePolicy, StateModel: options.StateModel, Visibility: options.Visibility, Routing: options.Routing})
	if err := servicemodel.Validate(policy); err != nil {
		return nil, fmt.Errorf("local capability %s 运行策略无效: %w", options.Capability, err)
	}
	if policy.Visibility != servicemodel.VisibilityLocal {
		return nil, errors.New("PrepareLocalRegistration 只接受 local visibility")
	}
	if options.InstanceID == "" {
		options.InstanceID = r.NodeID + "." + options.UnitID + "." + randomID()
	}
	registrationCtx, cancel := context.WithCancel(r.ctx)
	registration := &Registration{
		router: r, localOnly: true, record: Announcement{SchemaVersion: announcementSchemaVersion, Capability: options.Capability, ExtensionPoint: options.ExtensionPoint, ServiceRole: options.ServiceRole, LogicalService: options.LogicalService, RoutingDomain: options.RoutingDomain, PartitionKey: options.PartitionKey, InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel, Visibility: policy.Visibility, Routing: policy.Routing, Readiness: "ready", InstanceID: options.InstanceID, NodeID: r.NodeID, UnitID: options.UnitID, Artifact: ArtifactIdentity{PluginID: options.PluginID, Version: options.ArtifactVersion, SHA256: options.ArtifactSHA256}, Contract: ContractIdentity{Capability: options.Capability, Version: options.ContractVersion, InterfaceFingerprint: options.InterfaceFingerprint}, Subject: controlplane.RPCSubjectForPartition(options.Capability, options.LogicalService, options.RoutingDomain, options.PartitionKey), Health: "starting", UpdatedAt: time.Now().UTC()}, key: controlplane.CapabilityKey(options.Capability, options.InstanceID), id: randomID(), handler: handler, cancel: cancel}
	_ = registrationCtx
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		return nil, errors.New("addressing router 已关闭")
	}
	r.registrations[registration.id] = registration
	r.mu.Unlock()
	return registration, nil
}

// Register 保持普通调用方的一步注册语义；需要候选原子发布的 Runtime 使用
// PrepareRegistration + ActivateRegistrations，把多条能力的可见性门闩一起打开。
func (r *Router) Register(ctx context.Context, options RegisterOptions, handler InvokeHandler) (*Registration, error) {
	registration, err := r.PrepareRegistration(ctx, options, handler)
	if err != nil {
		return nil, err
	}
	if err := ActivateRegistrations(ctx, []*Registration{registration}); err != nil {
		_ = registration.Close(context.Background())
		return nil, err
	}
	return registration, nil
}

// PrepareRegistration 完成订阅和 starting 租约等所有可能失败的准备工作，但不把
// handler 放入本地路由，也不允许 NATS 回调进入插件。准备态只用于候选切换。
func (r *Router) PrepareRegistration(ctx context.Context, options RegisterOptions, handler InvokeHandler) (*Registration, error) {
	if options.Capability == "" || options.ExtensionPoint == "" || handler == nil {
		return nil, errors.New("capability、extension point 和 handler 不能为空")
	}
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: options.InstancePolicy, StateModel: options.StateModel,
		Visibility: options.Visibility, Routing: options.Routing,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return nil, fmt.Errorf("capability %s 运行策略无效: %w", options.Capability, err)
	}
	if policy.Visibility == servicemodel.VisibilityLocal {
		return nil, errors.New("local capability 不能注册到全局 addressing router")
	}
	if (policy.InstancePolicy == servicemodel.PolicyLeader || policy.InstancePolicy == servicemodel.PolicyPartitioned) && options.FencingToken == "" {
		return nil, fmt.Errorf("%s capability 必须携带 fencing token", policy.InstancePolicy)
	}
	options.InstancePolicy, options.StateModel = policy.InstancePolicy, policy.StateModel
	options.Visibility, options.Routing = policy.Visibility, policy.Routing
	if options.InstanceID == "" {
		options.InstanceID = r.NodeID + "." + options.UnitID + "." + randomID()
	}
	readiness := options.Readiness
	if readiness == "" {
		readiness = "ready"
	}
	leaseExpiresAt := options.LeaseExpiresAt
	if leaseExpiresAt.IsZero() {
		leaseExpiresAt = time.Now().UTC().Add(30 * time.Second)
	}
	record := Announcement{
		SchemaVersion: announcementSchemaVersion, Capability: options.Capability, ExtensionPoint: options.ExtensionPoint,
		ServiceRole: options.ServiceRole, LogicalService: options.LogicalService, RoutingDomain: options.RoutingDomain, PartitionKey: options.PartitionKey,
		InstancePolicy: options.InstancePolicy, StateModel: options.StateModel,
		Visibility: options.Visibility, Routing: options.Routing,
		Readiness: readiness, ReadinessReason: options.ReadinessReason,
		Generation: options.Generation, FencingToken: options.FencingToken, LeaseExpiresAt: leaseExpiresAt,
		InstanceID: options.InstanceID, NodeID: r.NodeID,
		UnitID:   options.UnitID,
		Artifact: ArtifactIdentity{PluginID: options.PluginID, Version: options.ArtifactVersion, SHA256: options.ArtifactSHA256},
		Contract: ContractIdentity{Capability: options.Capability, Version: options.ContractVersion, InterfaceFingerprint: options.InterfaceFingerprint},
		Subject:  controlplane.RPCSubjectForPartition(options.Capability, options.LogicalService, options.RoutingDomain, options.PartitionKey), Health: "starting", UpdatedAt: time.Now().UTC(),
	}
	registrationID := randomID()
	registrationCtx, cancel := context.WithCancel(r.ctx)
	registration := &Registration{
		router: r, record: record, key: controlplane.CapabilityKey(options.Capability, options.InstanceID),
		id: registrationID, handler: handler, cancel: cancel,
	}
	sub, err := r.NC.QueueSubscribe(record.Subject, controlplane.RPCQueueForPartition(options.Capability, options.LogicalService, options.RoutingDomain, options.PartitionKey), func(msg *nats.Msg) {
		current, accepting := registration.acceptingRecord()
		if !accepting {
			r.respondTransportError(msg, errorcode.RemoteInvokeFailed, "候选 capability 尚未激活", true, "")
			return
		}
		r.serveInvoke(registrationID, current, handler, msg)
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("订阅远端 capability %s: %w", options.Capability, err)
	}
	registration.sub = sub
	limits := r.Limits.Normalize()
	if err := sub.SetPendingLimits(limits.MaxPendingRequests, limits.MaxPendingRequests*limits.MaxMessageBytes()); err != nil {
		cancel()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("配置 capability 有界 pending 队列: %w", err)
	}
	directSub, err := r.NC.Subscribe(controlplane.RPCInstanceSubject(options.Capability, options.LogicalService, options.RoutingDomain, options.PartitionKey, options.InstanceID), func(msg *nats.Msg) {
		current, accepting := registration.acceptingRecord()
		if !accepting {
			r.respondTransportError(msg, errorcode.RemoteInvokeFailed, "候选 capability 尚未激活", true, "")
			return
		}
		r.serveInvoke(registrationID, current, handler, msg)
	})
	if err != nil {
		cancel()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("订阅 capability 实例 %s: %w", options.InstanceID, err)
	}
	registration.directSub = directSub
	if err := directSub.SetPendingLimits(limits.MaxPendingRequests, limits.MaxPendingRequests*limits.MaxMessageBytes()); err != nil {
		cancel()
		_ = directSub.Unsubscribe()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("配置 capability 实例有界 pending 队列: %w", err)
	}
	flushCtx, flushCancel := deadlineContext(ctx, 5*time.Second)
	defer flushCancel()
	if err := r.NC.FlushWithContext(flushCtx); err != nil {
		cancel()
		_ = directSub.Unsubscribe()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("确认 capability 订阅: %w", err)
	}
	if err := r.putAnnouncement(ctx, r.Directory, registration.key, record); err != nil {
		cancel()
		_ = directSub.Unsubscribe()
		_ = sub.Unsubscribe()
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		_ = directSub.Unsubscribe()
		_ = sub.Unsubscribe()
		_ = r.Directory.Delete(ctx, registration.key)
		return nil, errors.New("addressing router 已关闭")
	}
	r.registrations[registrationID] = registration
	r.mu.Unlock()
	go registration.heartbeat(registrationCtx)
	return registration, nil
}

func (registration *Registration) acceptingRecord() (Announcement, bool) {
	if registration == nil || !registration.active.Load() {
		return Announcement{}, false
	}
	registration.recordMu.Lock()
	defer registration.recordMu.Unlock()
	record := registration.record
	return record, record.Health == "healthy" && (record.Readiness == "" || record.Readiness == "ready" || record.Readiness == "degraded")
}

// ActivateRegistrations 先把整组租约改为 healthy，全部成功后才在一个临界区加入
// 本地路由并打开共享门闩。任何租约发布失败都会恢复 starting，候选不会处理调用。
