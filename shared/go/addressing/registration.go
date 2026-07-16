package addressing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
)

// RegisterOptions 描述一个可被本地直调和远端 queue group 调用的实例。
type RegisterOptions struct {
	Capability     string
	ExtensionPoint string
	ServiceRole    string
	UnitID         string
	Version        string
	InstanceID     string
}

type Registration struct {
	router   *Router
	record   Announcement
	recordMu sync.Mutex
	handler  InvokeHandler
	key      string
	id       string
	sub      *nats.Subscription
	stream   bool
	cancel   context.CancelFunc
	active   atomic.Bool
	once     sync.Once
	closeErr error
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
	if options.InstanceID == "" {
		options.InstanceID = r.NodeID + "." + options.UnitID + "." + randomID()
	}
	record := Announcement{
		SchemaVersion: 1, Capability: options.Capability, ExtensionPoint: options.ExtensionPoint,
		ServiceRole: options.ServiceRole, InstanceID: options.InstanceID, NodeID: r.NodeID,
		UnitID: options.UnitID, Version: options.Version,
		Subject: controlplane.RPCSubject(options.Capability), Health: "starting", UpdatedAt: time.Now().UTC(),
	}
	registrationID := randomID()
	registrationCtx, cancel := context.WithCancel(r.ctx)
	registration := &Registration{
		router: r, record: record, key: controlplane.CapabilityKey(options.Capability, options.InstanceID),
		id: registrationID, handler: handler, cancel: cancel,
	}
	sub, err := r.NC.QueueSubscribe(record.Subject, controlplane.RPCQueue(options.Capability), func(msg *nats.Msg) {
		if !registration.active.Load() {
			r.respondTransportError(msg, errorcode.RemoteInvokeFailed, "候选 capability 尚未激活", true, "")
			return
		}
		r.serveInvoke(registrationID, options.Capability, handler, msg)
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
	flushCtx, flushCancel := deadlineContext(ctx, 5*time.Second)
	defer flushCancel()
	if err := r.NC.FlushWithContext(flushCtx); err != nil {
		cancel()
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("确认 capability 订阅: %w", err)
	}
	if err := r.putAnnouncement(ctx, r.Directory, registration.key, record); err != nil {
		cancel()
		_ = sub.Unsubscribe()
		return nil, err
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		_ = sub.Unsubscribe()
		_ = r.Directory.Delete(ctx, registration.key)
		return nil, errors.New("addressing router 已关闭")
	}
	r.registrations[registrationID] = registration
	r.mu.Unlock()
	go registration.heartbeat(registrationCtx)
	return registration, nil
}

// ActivateRegistrations 先把整组租约改为 healthy，全部成功后才在一个临界区加入
// 本地路由并打开共享门闩。任何租约发布失败都会恢复 starting，候选不会处理调用。
func ActivateRegistrations(ctx context.Context, registrations []*Registration) error {
	if len(registrations) == 0 {
		return nil
	}
	router := registrations[0].router
	if router == nil {
		return errors.New("registration 缺少 router")
	}
	seen := make(map[*Registration]struct{}, len(registrations))
	allActive := true
	for _, registration := range registrations {
		if registration == nil || registration.router != router {
			return errors.New("registration group 必须来自同一个 router")
		}
		if _, duplicate := seen[registration]; duplicate {
			return errors.New("registration group 不能包含重复项")
		}
		seen[registration] = struct{}{}
		allActive = allActive && registration.active.Load()
	}
	if allActive {
		return nil
	}
	for _, registration := range registrations {
		if registration.active.Load() {
			return errors.New("registration group 不能混合已激活和准备态")
		}
		registration.recordMu.Lock()
	}
	defer func() {
		for index := len(registrations) - 1; index >= 0; index-- {
			registrations[index].recordMu.Unlock()
		}
	}()

	activated := make([]*Registration, 0, len(registrations))
	for _, registration := range registrations {
		record := registration.record
		record.Health = "healthy"
		record.UpdatedAt = time.Now().UTC()
		if err := router.putAnnouncement(ctx, router.Directory, registration.key, record); err != nil {
			for _, previous := range activated {
				rollback := previous.record
				rollback.Health = "starting"
				rollback.UpdatedAt = time.Now().UTC()
				_ = router.putAnnouncement(context.Background(), router.Directory, previous.key, rollback)
				previous.record = rollback
			}
			return fmt.Errorf("激活 capability %s: %w", registration.record.Capability, err)
		}
		registration.record = record
		activated = append(activated, registration)
	}

	router.mu.Lock()
	if router.closed {
		router.mu.Unlock()
		for _, registration := range activated {
			record := registration.record
			record.Health = "starting"
			_ = router.putAnnouncement(context.Background(), router.Directory, registration.key, record)
			registration.record = record
		}
		return errors.New("addressing router 已关闭")
	}
	for _, registration := range registrations {
		capability := registration.record.Capability
		router.local[capability] = append(router.local[capability], localHandler{
			registrationID: registration.id, handler: registration.handler,
		})
		registration.active.Store(true)
	}
	router.mu.Unlock()
	return nil
}

func (r *Router) serveInvoke(registrationID, capability string, handler InvokeHandler, msg *nats.Msg) {
	if msg.Reply == "" {
		return
	}
	limits := r.Limits.Normalize()
	if len(msg.Data) > limits.MaxMessageBytes() {
		r.respondTransportError(msg, errorcode.PayloadTooLarge, "请求信封超过协议消息上限", false, "")
		return
	}
	var identity TransportIdentity
	if r.Transport != nil {
		var err error
		identity, err = r.Transport.verifyMessage(msg)
		if err != nil {
			r.respondTransportError(msg, errorcode.PermissionDenied, "远端调用身份校验失败", false, "")
			return
		}
	}
	var req addressingv1.InvokeRequest
	if err := proto.Unmarshal(msg.Data, &req); err != nil {
		r.respondTransportError(msg, errorcode.WireInvalidRequest, err.Error(), false, "")
		return
	}
	if req.Target == nil || req.Target.Capability != capability {
		r.respondTransportError(msg, errorcode.WireTargetMismatch, "请求 capability 与 subject 不一致", false, req.RequestId)
		return
	}
	if !limits.PayloadAllowed(req.Payload) {
		r.respondTransportError(msg, errorcode.PayloadTooLarge, "请求 payload 超过上限", false, req.RequestId)
		return
	}
	if !limits.MetadataAllowed(proto.Size(req.Context)) {
		r.respondTransportError(msg, errorcode.MetadataTooLarge, "请求 CallContext 超过 metadata 上限", false, req.RequestId)
		return
	}
	if r.Transport != nil {
		authenticated, err := authenticatedTransportContext(identity, req.Context)
		if err != nil {
			r.respondTransportError(msg, errorcode.PermissionDenied, err.Error(), false, req.RequestId)
			return
		}
		req.Context = authenticated
	}
	handlerCtx, boundedCallCtx, cancel := r.boundedCallContext(r.ctx, req.Context)
	if !r.enterHandlerCall() {
		cancel()
		r.respondTransportError(msg, errorcode.ConcurrencyLimited, "addressing handler 并发达到上限", true, req.RequestId)
		return
	}
	r.mu.Lock()
	r.inflight[req.RequestId] = cancel
	if _, canceled := r.pendingCancels[req.RequestId]; canceled {
		delete(r.pendingCancels, req.RequestId)
		cancel()
	}
	r.mu.Unlock()
	defer func() {
		cancel()
		r.mu.Lock()
		delete(r.inflight, req.RequestId)
		r.mu.Unlock()
		r.leaveHandlerCall()
	}()
	var finish func(string, error)
	if r.Observer != nil {
		boundedCallCtx, finish = r.Observer.BeginCall(handlerCtx, boundedCallCtx, "addressing.handler", map[string]string{"transport": "nats"})
	}
	result, payload, err := handler(handlerCtx, req.Target, boundedCallCtx, req.Payload)
	if finish != nil {
		status := "transport_error"
		if err == nil && result != nil {
			status = result.Status.String()
		}
		finish(status, err)
	}
	response := &addressingv1.InvokeResponse{RequestId: req.RequestId, Result: result, Payload: payload}
	if err != nil {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteInvokeFailed, Message: err.Error(), Retryable: true}
	} else if result == nil {
		response.TransportError = &addressingv1.TransportError{Code: errorcode.RemoteInvalidResponse, Message: "handler 未返回 CallResult"}
	} else if !limits.PayloadAllowed(payload) {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: errorcode.PayloadTooLarge, Message: "handler 响应 payload 超过上限"}
	}
	raw, marshalErr := proto.Marshal(response)
	if marshalErr != nil {
		r.Logf("编码 capability 响应失败 id=%s: %v", registrationID, marshalErr)
		return
	}
	responseMessage := nats.NewMsg(msg.Reply)
	responseMessage.Data = raw
	if r.Transport != nil {
		if err := r.Transport.signMessage(responseMessage); err != nil {
			r.Logf("签名 capability 响应失败 id=%s: %v", registrationID, err)
			return
		}
	}
	if err := msg.RespondMsg(responseMessage); err != nil {
		r.Logf("回应 capability %s 失败: %v", capability, err)
	}
}

func (r *Router) respondTransportError(msg *nats.Msg, code, message string, retryable bool, requestID string) {
	raw, _ := proto.Marshal(&addressingv1.InvokeResponse{
		RequestId:      requestID,
		TransportError: &addressingv1.TransportError{Code: code, Message: message, Retryable: retryable},
	})
	response := nats.NewMsg(msg.Reply)
	response.Data = raw
	if r.Transport != nil {
		if err := r.Transport.signMessage(response); err != nil {
			return
		}
	}
	_ = msg.RespondMsg(response)
}

func (registration *Registration) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			registration.recordMu.Lock()
			record := registration.record
			record.UpdatedAt = time.Now().UTC()
			heartbeatCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := registration.router.putAnnouncement(heartbeatCtx, registration.router.Directory, registration.key, record)
			cancel()
			if err == nil {
				registration.record = record
			}
			registration.recordMu.Unlock()
			if err != nil {
				registration.router.Logf("刷新 capability 租约失败 %s: %v", record.Capability, err)
			}
		}
	}
}

func (registration *Registration) Close(ctx context.Context) error {
	registration.once.Do(func() {
		registration.active.Store(false)
		registration.cancel()
		registration.recordMu.Lock()
		record := registration.record
		registration.recordMu.Unlock()
		registration.router.mu.Lock()
		locals := registration.router.local[record.Capability]
		for index := range locals {
			if locals[index].registrationID != registration.id {
				continue
			}
			locals = append(locals[:index], locals[index+1:]...)
			break
		}
		if len(locals) == 0 {
			delete(registration.router.local, record.Capability)
			delete(registration.router.localCursor, record.Capability)
		} else {
			registration.router.local[record.Capability] = locals
		}
		if registration.stream {
			streams := registration.router.streamLocal[record.Capability]
			for index := range streams {
				if streams[index].registrationID == registration.id {
					streams = append(streams[:index], streams[index+1:]...)
					break
				}
			}
			if len(streams) == 0 {
				delete(registration.router.streamLocal, record.Capability)
				delete(registration.router.streamCursor, record.Capability)
			} else {
				registration.router.streamLocal[record.Capability] = streams
			}
		}
		delete(registration.router.registrations, registration.id)
		if instances := registration.router.instances[record.Capability]; instances != nil {
			delete(instances, registration.key)
			if len(instances) == 0 {
				delete(registration.router.instances, record.Capability)
			}
		}
		registration.router.mu.Unlock()
		if err := registration.router.Directory.Delete(ctx, registration.key); err != nil {
			if !errors.Is(err, jetstream.ErrKeyNotFound) {
				registration.closeErr = errors.Join(registration.closeErr, err)
			}
		}
		if registration.sub != nil {
			registration.closeErr = errors.Join(registration.closeErr, registration.sub.Drain())
		}
	})
	return registration.closeErr
}

func (r *Router) putAnnouncement(ctx context.Context, directory interface {
	Put(context.Context, string, []byte) (uint64, error)
}, key string, record Announcement) error {
	prepared, err := r.prepareAnnouncement(key, record)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return err
	}
	if _, err := directory.Put(ctx, key, raw); err != nil {
		return fmt.Errorf("发布 capability 租约 %s: %w", record.Capability, err)
	}
	return nil
}

// HostHandler 把协议宿主适配成寻址层 handler，不复制业务契约。
func HostHandler(invoke func(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)) InvokeHandler {
	return invoke
}
