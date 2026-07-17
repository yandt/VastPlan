// Package addressing 实现后端服务间位置透明的 capability 寻址。
//
// 本地命中走函数直调；远端走 NATS request-reply。业务签名始终是
// CallTarget + CallContext + payload，传输差异不泄漏给调用方。
package addressing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/observability"
	"cdsoft.com.cn/VastPlan/shared/go/protocollimit"
	"cdsoft.com.cn/VastPlan/shared/go/servicemodel"
)

const cancelSubject = "vp.rpc.cancel.v1"

var ErrCapabilityNotFound = errors.New("全局能力目录中没有健康实例")

// InvokeHandler 是本地与远端服务实现共用的处理签名。
type InvokeHandler func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

type EventHandler func(context.Context, *contractv1.CallContext, *contractv1.CallEvent) error

// Announcement 是全局能力目录的一条实例租约。
type Announcement struct {
	SchemaVersion      int       `json:"schema_version"`
	Capability         string    `json:"capability"`
	ExtensionPoint     string    `json:"extension_point"`
	ServiceRole        string    `json:"service_role"`
	LogicalService     string    `json:"logical_service,omitempty"`
	RoutingDomain      string    `json:"routing_domain,omitempty"`
	PartitionKey       string    `json:"partition_key,omitempty"`
	InstancePolicy     string    `json:"instance_policy,omitempty"`
	StateModel         string    `json:"state_model,omitempty"`
	Visibility         string    `json:"visibility,omitempty"`
	Routing            string    `json:"routing,omitempty"`
	InstanceID         string    `json:"instance_id"`
	NodeID             string    `json:"node_id"`
	UnitID             string    `json:"unit_id"`
	Subject            string    `json:"subject"`
	StreamEndpoint     string    `json:"stream_endpoint,omitempty"`
	Version            string    `json:"version,omitempty"`
	Health             string    `json:"health"`
	Readiness          string    `json:"readiness,omitempty"`
	ReadinessReason    string    `json:"readiness_reason,omitempty"`
	Generation         uint64    `json:"generation,omitempty"`
	FencingToken       string    `json:"fencing_token,omitempty"`
	LeaseExpiresAt     time.Time `json:"lease_expires_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at"`
	TransportPublicKey string    `json:"transport_public_key,omitempty"`
	TransportTimestamp string    `json:"transport_timestamp,omitempty"`
	TransportNonce     string    `json:"transport_nonce,omitempty"`
	TransportSignature string    `json:"transport_signature,omitempty"`
}

type localHandler struct {
	registrationID string
	handler        InvokeHandler
}

type localStreamHandler struct {
	registrationID string
	handler        StreamHandler
}

// Router 同时持有本地 fast path、NATS 数据面和能力目录缓存。
type Router struct {
	NC          *nats.Conn
	Directory   jetstream.KeyValue
	JetStream   jetstream.JetStream
	Events      jetstream.Stream
	NodeID      string
	CallTimeout time.Duration
	// Limits 约束一元调用和流式调用的资源占用。CallTimeout 仅作为旧配置兼容覆盖项。
	Limits   protocollimit.Limits
	Logf     func(string, ...any)
	Observer *observability.Observer
	// Transport 在生产模式下必须配置：它对 NATS/stream 信封签名，并在处理前
	// 重建权威工作负载身份。nil 仅保留给显式本地开发和测试。
	Transport *TransportSecurity

	ctx    context.Context
	cancel context.CancelFunc

	mu             sync.RWMutex
	local          map[string][]localHandler
	localCursor    map[string]uint64
	streamLocal    map[string][]localStreamHandler
	streamCursor   map[string]uint64
	streamResolve  map[string]uint64
	instances      map[string]map[string]Announcement // capability -> directory key -> record
	registrations  map[string]*Registration
	inflight       map[string]context.CancelFunc
	outboundCalls  int
	activeCalls    int
	pendingCancels map[string]time.Time
	closed         bool
	closeOnce      sync.Once
	cancelSub      *nats.Subscription
	directoryW     jetstream.KeyWatcher
	streamServer   *grpc.Server
	streamListener net.Listener
	streamEndpoint string
	streamCreds    credentials.TransportCredentials
	streamInsecure bool
}

func NewRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any)) (*Router, error) {
	return newRouter(nc, directory, nodeID, logf, nil)
}

func NewSecureRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any), security *TransportSecurity) (*Router, error) {
	if security == nil {
		return nil, errors.New("生产 addressing router 必须配置传输身份")
	}
	identity := security.SelfIdentity()
	if identity.NodeID == "" || identity.NodeID != nodeID {
		return nil, fmt.Errorf("传输身份 nodeID %q 与 router nodeID %q 不一致", identity.NodeID, nodeID)
	}
	return newRouter(nc, directory, nodeID, logf, security)
}

func newRouter(nc *nats.Conn, directory jetstream.KeyValue, nodeID string, logf func(string, ...any), security *TransportSecurity) (*Router, error) {
	if nc == nil || directory == nil || nodeID == "" {
		return nil, errors.New("addressing router 的 NATS、目录和 node id 必须配置")
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	js, err := jetstream.New(nc)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("初始化 JetStream: %w", err)
	}
	events, err := js.Stream(ctx, controlplane.EventsStream)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("打开持久事件 stream: %w", err)
	}
	r := &Router{
		NC: nc, Directory: directory, JetStream: js, Events: events,
		NodeID: nodeID, Limits: protocollimit.Default(), Logf: logf, Observer: observability.New(nil, nil), Transport: security,
		ctx: ctx, cancel: cancel, local: map[string][]localHandler{}, localCursor: map[string]uint64{},
		streamLocal: map[string][]localStreamHandler{}, streamCursor: map[string]uint64{},
		streamResolve: map[string]uint64{},
		instances:     map[string]map[string]Announcement{}, registrations: map[string]*Registration{},
		inflight: map[string]context.CancelFunc{}, pendingCancels: map[string]time.Time{},
	}
	sub, err := nc.Subscribe(cancelSubject, r.handleCancel)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("订阅取消信号: %w", err)
	}
	r.cancelSub = sub
	if err := nc.Flush(); err != nil {
		_ = sub.Unsubscribe()
		cancel()
		return nil, fmt.Errorf("确认取消订阅: %w", err)
	}
	if err := r.startDirectoryWatch(); err != nil {
		_ = sub.Unsubscribe()
		cancel()
		return nil, err
	}
	go r.directoryRefreshLoop()
	return r, nil
}

func (r *Router) Invoke(ctx context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, payload []byte) (result *contractv1.CallResult, responsePayload []byte, callErr error) {
	if target == nil || target.Capability == "" {
		return nil, nil, errors.New("调用目标 capability 不能为空")
	}
	limits := r.Limits.Normalize()
	if !limits.PayloadAllowed(payload) {
		return nil, nil, &TransportError{Code: errorcode.PayloadTooLarge,
			Message: fmt.Sprintf("payload 为 %d bytes，超过上限 %d bytes", len(payload), limits.MaxPayloadBytes)}
	}
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return nil, nil, &TransportError{Code: errorcode.MetadataTooLarge,
			Message: fmt.Sprintf("CallContext 为 %d bytes，超过 metadata 上限 %d bytes", proto.Size(callCtx), limits.MaxMetadataBytes)}
	}
	ctx, callCtx, cancel := r.boundedCallContext(ctx, callCtx)
	defer cancel()
	if r.Observer != nil {
		var finish func(string, error)
		callCtx, finish = r.Observer.BeginCall(ctx, callCtx, "addressing.invoke", map[string]string{"transport": "auto"})
		defer func() {
			status := "transport_error"
			if callErr == nil && result != nil {
				status = result.Status.String()
			}
			finish(status, callErr)
		}()
	}
	if !r.enterOutboundCall() {
		return nil, nil, &TransportError{Code: errorcode.ConcurrencyLimited, Message: "addressing 调用并发达到上限", Retryable: true}
	}
	defer r.leaveOutboundCall()
	r.mu.Lock()
	locals := r.local[target.Capability]
	var local localHandler
	if len(locals) > 0 {
		cursor := r.localCursor[target.Capability]
		local = locals[cursor%uint64(len(locals))]
		r.localCursor[target.Capability] = cursor + 1
	}
	r.mu.Unlock()
	if local.handler != nil {
		result, out, err := local.handler(ctx, target, callCtx, payload)
		if err == nil && !limits.PayloadAllowed(out) {
			return nil, nil, &TransportError{Code: errorcode.PayloadTooLarge,
				Message: fmt.Sprintf("响应 payload 为 %d bytes，超过上限 %d bytes", len(out), limits.MaxPayloadBytes)}
		}
		return result, out, err
	}
	if len(r.instancesFor(target.Capability, target.GetLogicalService(), target.GetRoutingDomain(), target.GetPartitionKey())) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrCapabilityNotFound, target.Capability)
	}
	requestID := randomID()
	req := &addressingv1.InvokeRequest{RequestId: requestID, Target: target, Context: callCtx, Payload: payload}
	raw, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("编码远端调用: %w", err)
	}
	subject := controlplane.RPCSubjectForPartition(target.Capability, target.GetLogicalService(), target.GetRoutingDomain(), target.GetPartitionKey())
	request := nats.NewMsg(subject)
	request.Data = raw
	if r.Transport != nil {
		if err := r.Transport.signMessage(request); err != nil {
			return nil, nil, err
		}
	}
	msg, err := r.NC.RequestMsgWithContext(ctx, request)
	if err != nil {
		if ctx.Err() != nil {
			_ = r.NC.Publish(cancelSubject, []byte(requestID))
		}
		return nil, nil, fmt.Errorf("NATS 调用 %s: %w", target.Capability, err)
	}
	if r.Transport != nil {
		if _, err := r.Transport.verifyMessage(msg); err != nil {
			return nil, nil, fmt.Errorf("验证 NATS 响应身份: %w", err)
		}
	}
	var resp addressingv1.InvokeResponse
	if err := proto.Unmarshal(msg.Data, &resp); err != nil {
		return nil, nil, fmt.Errorf("解码远端响应: %w", err)
	}
	if resp.RequestId != requestID {
		return nil, nil, fmt.Errorf("远端响应 request_id 不匹配: %s", resp.RequestId)
	}
	if failure := resp.TransportError; failure != nil {
		return nil, nil, &TransportError{Code: failure.Code, Message: failure.Message, Retryable: failure.Retryable}
	}
	if resp.Result == nil {
		return nil, nil, errors.New("远端响应缺少 CallResult")
	}
	if !limits.PayloadAllowed(resp.Payload) {
		return nil, nil, &TransportError{Code: errorcode.PayloadTooLarge,
			Message: fmt.Sprintf("远端响应 payload 为 %d bytes，超过上限 %d bytes", len(resp.Payload), limits.MaxPayloadBytes)}
	}
	return resp.Result, resp.Payload, nil
}

func (r *Router) callTimeout() time.Duration {
	if r.CallTimeout > 0 {
		return r.CallTimeout
	}
	return r.Limits.Normalize().DefaultDeadline
}

func (r *Router) enterOutboundCall() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.outboundCalls >= r.Limits.Normalize().MaxConcurrentCalls {
		return false
	}
	r.outboundCalls++
	return true
}

func (r *Router) leaveOutboundCall() {
	r.mu.Lock()
	if r.outboundCalls > 0 {
		r.outboundCalls--
	}
	r.mu.Unlock()
}

func (r *Router) enterHandlerCall() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeCalls >= r.Limits.Normalize().MaxConcurrentCalls {
		return false
	}
	r.activeCalls++
	return true
}

func (r *Router) leaveHandlerCall() {
	r.mu.Lock()
	if r.activeCalls > 0 {
		r.activeCalls--
	}
	r.mu.Unlock()
}

func (r *Router) boundedCallContext(ctx context.Context, callCtx *contractv1.CallContext) (context.Context, *contractv1.CallContext, context.CancelFunc) {
	deadline := time.Now().Add(r.callTimeout())
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	if callCtx != nil && callCtx.DeadlineUnixMs != nil {
		declared := time.UnixMilli(*callCtx.DeadlineUnixMs)
		if declared.Before(deadline) {
			deadline = declared
		}
	}
	bounded := &contractv1.CallContext{}
	if callCtx != nil {
		bounded = proto.Clone(callCtx).(*contractv1.CallContext)
	}
	deadlineUnixMs := deadline.UnixMilli()
	bounded.DeadlineUnixMs = &deadlineUnixMs
	boundedCtx, cancel := context.WithDeadline(ctx, deadline)
	return boundedCtx, bounded, cancel
}

// Publish 使用 Core NATS 发布非持久事件；需持久化/至少一次的事件后续显式使用 JetStream。
func (r *Router) Publish(ctx context.Context, callCtx *contractv1.CallContext, event *contractv1.CallEvent) error {
	if event == nil || event.Type == "" {
		return errors.New("事件 type 不能为空")
	}
	limits := r.Limits.Normalize()
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return &TransportError{Code: errorcode.MetadataTooLarge, Message: "事件 CallContext 超过 metadata 上限"}
	}
	if !limits.PayloadAllowed(event.Payload) {
		return &TransportError{Code: errorcode.PayloadTooLarge, Message: "事件 payload 超过上限"}
	}
	raw, err := proto.Marshal(&addressingv1.EventEnvelope{Context: callCtx, Event: event})
	if err != nil {
		return err
	}
	message := nats.NewMsg(controlplane.EventSubject(event.Type))
	message.Data = raw
	if r.Transport != nil {
		if err := r.Transport.signMessage(message); err != nil {
			return err
		}
	}
	if err := r.NC.PublishMsg(message); err != nil {
		return err
	}
	flushCtx, cancel := deadlineContext(ctx, 5*time.Second)
	defer cancel()
	return r.NC.FlushWithContext(flushCtx)
}

func (r *Router) Subscribe(eventType string, handler EventHandler) (*Subscription, error) {
	if eventType == "" || handler == nil {
		return nil, errors.New("事件类型和 handler 不能为空")
	}
	subject := controlplane.EventSubject(eventType)
	if eventType == ">" {
		subject = "vp.event.v1.>"
	}
	sub, err := r.NC.Subscribe(subject, func(msg *nats.Msg) {
		limits := r.Limits.Normalize()
		if len(msg.Data) > limits.MaxMessageBytes() {
			r.Logf("丢弃超过协议消息上限的事件 subject=%s", msg.Subject)
			return
		}
		var identity TransportIdentity
		if r.Transport != nil {
			var err error
			identity, err = r.Transport.verifyMessage(msg)
			if err != nil {
				r.Logf("丢弃身份无效的事件 subject=%s: %v", msg.Subject, err)
				return
			}
		}
		var envelope addressingv1.EventEnvelope
		if err := proto.Unmarshal(msg.Data, &envelope); err != nil {
			r.Logf("丢弃非法事件信封 subject=%s: %v", msg.Subject, err)
			return
		}
		if !limits.MetadataAllowed(proto.Size(envelope.Context)) || envelope.Event == nil || !limits.PayloadAllowed(envelope.Event.Payload) {
			r.Logf("丢弃超过资源边界的事件 subject=%s", msg.Subject)
			return
		}
		if r.Transport != nil {
			authenticated, err := authenticatedTransportContext(identity, envelope.Context)
			if err != nil {
				r.Logf("丢弃租户身份不一致的事件 subject=%s: %v", msg.Subject, err)
				return
			}
			envelope.Context = authenticated
		}
		if err := handler(r.ctx, envelope.Context, envelope.Event); err != nil {
			r.Logf("事件 handler 失败 type=%s: %v", envelope.Event.GetType(), err)
		}
	})
	if err != nil {
		return nil, err
	}
	limits := r.Limits.Normalize()
	if err := sub.SetPendingLimits(limits.MaxPendingRequests, limits.MaxPendingRequests*limits.MaxMessageBytes()); err != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("配置事件有界 pending 队列: %w", err)
	}
	if err := r.NC.Flush(); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	return &Subscription{sub: sub}, nil
}

func (r *Router) Instances(capability string) []Announcement {
	return r.InstancesFor(capability, "", "")
}

// HasLocal 用于同节点依赖 gate；local capability 刻意不进入全局目录，但仍必须可被
// 后续 unit 观察到。它只报告已激活的本地 handler。
func (r *Router) HasLocal(capability string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.local[capability]) > 0
}

// InstancesFor 只返回指定逻辑服务和路由域中的健康实例。空过滤条件保持 v1 兼容行为。
func (r *Router) InstancesFor(capability, logicalService, routingDomain string) []Announcement {
	return r.instancesFor(capability, logicalService, routingDomain, "")
}

func (r *Router) instancesFor(capability, logicalService, routingDomain, partitionKey string) []Announcement {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := r.instances[capability]
	out := make([]Announcement, 0, len(entries))
	for _, entry := range entries {
		if entry.Health == "healthy" && (logicalService == "" || entry.LogicalService == logicalService) &&
			(routingDomain == "" || entry.RoutingDomain == routingDomain) &&
			(partitionKey == "" || entry.PartitionKey == partitionKey) &&
			(entry.Readiness == "" || entry.Readiness == "ready" || entry.Readiness == "degraded") {
			out = append(out, entry)
		}
	}
	return out
}

// WaitReady 等待指定 capability 至少出现一个可接收调用的 readiness lease。
func (r *Router) WaitReady(ctx context.Context, capability, logicalService, routingDomain string) (Announcement, error) {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		instances := r.InstancesFor(capability, logicalService, routingDomain)
		if len(instances) > 0 {
			return instances[0], nil
		}
		select {
		case <-ctx.Done():
			return Announcement{}, fmt.Errorf("等待 capability %s readiness: %w", capability, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Router) prepareAnnouncement(key string, record Announcement) (Announcement, error) {
	if err := validateAnnouncementShape(key, record); err != nil {
		return Announcement{}, err
	}
	if record.NodeID != r.NodeID {
		return Announcement{}, fmt.Errorf("能力目录 node_id %q 与 router node_id %q 不一致", record.NodeID, r.NodeID)
	}
	if r.Transport == nil {
		return record, nil
	}
	identity := r.Transport.SelfIdentity()
	if identity.NodeID == "" || identity.NodeID != r.NodeID {
		return Announcement{}, errors.New("传输身份未绑定当前 router node_id")
	}
	return r.Transport.signAnnouncement(key, record)
}

func validateAnnouncementShape(key string, record Announcement) error {
	if key != controlplane.CapabilityKey(record.Capability, record.InstanceID) {
		return fmt.Errorf("能力目录 key 与记录身份不一致: %s", key)
	}
	if record.NodeID == "" {
		return errors.New("能力目录 node_id 不能为空")
	}
	if record.Subject != controlplane.RPCSubjectForPartition(record.Capability, record.LogicalService, record.RoutingDomain, record.PartitionKey) {
		return errors.New("能力目录 subject 与 capability 不一致")
	}
	if record.Readiness == "" {
		record.Readiness = "ready"
	}
	switch record.Readiness {
	case "ready", "degraded", "draining":
	default:
		return fmt.Errorf("能力目录 readiness 无效: %q", record.Readiness)
	}
	policy := servicemodel.Normalize(servicemodel.Policy{
		InstancePolicy: record.InstancePolicy, StateModel: record.StateModel,
		Visibility: record.Visibility, Routing: record.Routing,
	})
	if err := servicemodel.Validate(policy); err != nil {
		return fmt.Errorf("能力目录运行策略无效: %w", err)
	}
	if policy.Visibility == servicemodel.VisibilityLocal {
		return errors.New("local capability 不得写入全局能力目录")
	}
	if (policy.InstancePolicy == servicemodel.PolicyLeader || policy.InstancePolicy == servicemodel.PolicyPartitioned) && record.FencingToken == "" {
		return errors.New("leader/partitioned capability 缺少 fencing token")
	}
	return nil
}

func (r *Router) validateAnnouncement(key string, record Announcement) error {
	if err := validateAnnouncementShape(key, record); err != nil {
		return err
	}
	if r.Transport == nil {
		return nil
	}
	identity, err := r.Transport.verifyAnnouncement(key, record)
	if err != nil {
		return fmt.Errorf("能力目录传输签名无效: %w", err)
	}
	if identity.NodeID != r.NodeID {
		return fmt.Errorf("能力目录签名身份 node_id %q 与 router node_id %q 不一致", identity.NodeID, r.NodeID)
	}
	return nil
}

func (r *Router) Close() error {
	var closeErr error
	r.closeOnce.Do(func() {
		r.mu.Lock()
		r.closed = true
		registrations := make([]*Registration, 0, len(r.registrations))
		for _, registration := range r.registrations {
			registrations = append(registrations, registration)
		}
		r.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, registration := range registrations {
			closeErr = errors.Join(closeErr, registration.Close(ctx))
		}

		r.cancel()
		if r.streamServer != nil {
			r.streamServer.Stop()
		}
		if r.streamListener != nil {
			if err := r.streamListener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				closeErr = errors.Join(closeErr, err)
			}
		}
		if r.directoryW != nil {
			closeErr = errors.Join(closeErr, r.directoryW.Stop())
		}
		if r.cancelSub != nil {
			closeErr = errors.Join(closeErr, r.cancelSub.Unsubscribe())
		}
		r.mu.Lock()
		for _, cancel := range r.inflight {
			cancel()
		}
		r.inflight = map[string]context.CancelFunc{}
		r.pendingCancels = map[string]time.Time{}
		r.local = map[string][]localHandler{}
		r.localCursor = map[string]uint64{}
		r.streamLocal = map[string][]localStreamHandler{}
		r.streamCursor = map[string]uint64{}
		r.streamResolve = map[string]uint64{}
		r.mu.Unlock()
	})
	return closeErr
}

func randomID() string {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// TransportError 保持远端处理器的传输失败与应用层 CallResult 分离。
type TransportError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *TransportError) Error() string { return e.Code + ": " + e.Message }

type Subscription struct{ sub *nats.Subscription }

func (s *Subscription) Close() error {
	if s == nil || s.sub == nil {
		return nil
	}
	return s.sub.Drain()
}

func (r *Router) handleCancel(msg *nats.Msg) {
	requestID := string(msg.Data)
	if requestID == "" {
		return
	}
	r.mu.Lock()
	cancel := r.inflight[requestID]
	if cancel == nil {
		// 取消消息与请求走不同 subject，跨连接时可能先到；短暂记忆可关闭这个竞态窗口。
		r.pendingCancels[requestID] = time.Now().Add(time.Minute)
	}
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (r *Router) startDirectoryWatch() error {
	watcher, err := r.Directory.WatchAll(r.ctx)
	if err != nil {
		return fmt.Errorf("watch 能力目录: %w", err)
	}
	r.directoryW = watcher
	go func() {
		for entry := range watcher.Updates() {
			if entry == nil {
				continue
			}
			r.applyDirectoryEntry(entry)
		}
	}()
	return nil
}

func (r *Router) applyDirectoryEntry(entry jetstream.KeyValueEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if entry.Operation() != jetstream.KeyValuePut {
		for capability, instances := range r.instances {
			delete(instances, entry.Key())
			if len(instances) == 0 {
				delete(r.instances, capability)
			}
		}
		return
	}
	var announcement Announcement
	if err := json.Unmarshal(entry.Value(), &announcement); err != nil {
		r.Logf("忽略非法能力目录记录 key=%s: %v", entry.Key(), err)
		return
	}
	if err := r.validateAnnouncement(entry.Key(), announcement); err != nil {
		r.Logf("忽略未通过身份校验的能力目录记录 key=%s: %v", entry.Key(), err)
		return
	}
	if r.instances[announcement.Capability] == nil {
		r.instances[announcement.Capability] = map[string]Announcement{}
	}
	r.instances[announcement.Capability][entry.Key()] = announcement
}

func (r *Router) directoryRefreshLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.ctx.Done():
			return
		case <-ticker.C:
			r.refreshDirectory()
			r.expirePendingCancels()
		}
	}
}

func (r *Router) refreshDirectory() {
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Second)
	defer cancel()
	keys, err := r.Directory.Keys(ctx)
	if err != nil {
		if errors.Is(err, jetstream.ErrNoKeysFound) {
			r.mu.Lock()
			r.instances = map[string]map[string]Announcement{}
			r.mu.Unlock()
		}
		return
	}
	next := map[string]map[string]Announcement{}
	for _, key := range keys {
		entry, err := r.Directory.Get(ctx, key)
		if err != nil {
			continue
		}
		var announcement Announcement
		if json.Unmarshal(entry.Value(), &announcement) != nil {
			continue
		}
		if err := r.validateAnnouncement(key, announcement); err != nil {
			continue
		}
		if next[announcement.Capability] == nil {
			next[announcement.Capability] = map[string]Announcement{}
		}
		next[announcement.Capability][key] = announcement
	}
	r.mu.Lock()
	r.instances = next
	r.mu.Unlock()
}

func (r *Router) expirePendingCancels() {
	now := time.Now()
	r.mu.Lock()
	for requestID, expiresAt := range r.pendingCancels {
		if !expiresAt.After(now) {
			delete(r.pendingCancels, requestID)
		}
	}
	r.mu.Unlock()
}

func deadlineContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := parent.Deadline(); ok {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, timeout)
}
