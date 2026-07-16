package addressing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/shared/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/controlplane"
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
	key      string
	id       string
	sub      *nats.Subscription
	cancel   context.CancelFunc
	once     sync.Once
	closeErr error
}

func (r *Router) Register(ctx context.Context, options RegisterOptions, handler InvokeHandler) (*Registration, error) {
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
		Subject: controlplane.RPCSubject(options.Capability), Health: "healthy", UpdatedAt: time.Now().UTC(),
	}
	registrationID := randomID()
	sub, err := r.NC.QueueSubscribe(record.Subject, controlplane.RPCQueue(options.Capability), func(msg *nats.Msg) {
		r.serveInvoke(registrationID, options.Capability, handler, msg)
	})
	if err != nil {
		return nil, fmt.Errorf("订阅远端 capability %s: %w", options.Capability, err)
	}
	flushCtx, flushCancel := deadlineContext(ctx, 5*time.Second)
	defer flushCancel()
	if err := r.NC.FlushWithContext(flushCtx); err != nil {
		_ = sub.Unsubscribe()
		return nil, fmt.Errorf("确认 capability 订阅: %w", err)
	}
	key := controlplane.CapabilityKey(options.Capability, options.InstanceID)
	if err := putAnnouncement(ctx, r.Directory, key, record); err != nil {
		_ = sub.Unsubscribe()
		return nil, err
	}
	registrationCtx, cancel := context.WithCancel(r.ctx)
	registration := &Registration{router: r, record: record, key: key, id: registrationID, sub: sub, cancel: cancel}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		cancel()
		_ = sub.Unsubscribe()
		_ = r.Directory.Delete(ctx, key)
		return nil, errors.New("addressing router 已关闭")
	}
	r.local[options.Capability] = append(r.local[options.Capability], localHandler{registrationID: registrationID, handler: handler})
	r.registrations[registrationID] = registration
	r.mu.Unlock()
	go registration.heartbeat(registrationCtx)
	return registration, nil
}

func (r *Router) serveInvoke(registrationID, capability string, handler InvokeHandler, msg *nats.Msg) {
	if msg.Reply == "" {
		return
	}
	var req addressingv1.InvokeRequest
	if err := proto.Unmarshal(msg.Data, &req); err != nil {
		r.respondTransportError(msg, "wire.invalid_request", err.Error(), false, "")
		return
	}
	if req.Target == nil || req.Target.Capability != capability {
		r.respondTransportError(msg, "wire.target_mismatch", "请求 capability 与 subject 不一致", false, req.RequestId)
		return
	}
	handlerCtx := r.ctx
	var cancel context.CancelFunc
	if req.Context != nil && req.Context.DeadlineUnixMs != nil {
		handlerCtx, cancel = context.WithDeadline(handlerCtx, time.UnixMilli(*req.Context.DeadlineUnixMs))
	} else if r.CallTimeout > 0 {
		handlerCtx, cancel = context.WithTimeout(handlerCtx, r.CallTimeout)
	} else {
		handlerCtx, cancel = context.WithCancel(handlerCtx)
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
	}()
	result, payload, err := handler(handlerCtx, req.Target, req.Context, req.Payload)
	response := &addressingv1.InvokeResponse{RequestId: req.RequestId, Result: result, Payload: payload}
	if err != nil {
		response.Result = nil
		response.Payload = nil
		response.TransportError = &addressingv1.TransportError{Code: "remote.invoke_failed", Message: err.Error(), Retryable: true}
	} else if result == nil {
		response.TransportError = &addressingv1.TransportError{Code: "remote.invalid_response", Message: "handler 未返回 CallResult"}
	}
	raw, marshalErr := proto.Marshal(response)
	if marshalErr != nil {
		r.Logf("编码 capability 响应失败 id=%s: %v", registrationID, marshalErr)
		return
	}
	if err := msg.Respond(raw); err != nil {
		r.Logf("回应 capability %s 失败: %v", capability, err)
	}
}

func (r *Router) respondTransportError(msg *nats.Msg, code, message string, retryable bool, requestID string) {
	raw, _ := proto.Marshal(&addressingv1.InvokeResponse{
		RequestId:      requestID,
		TransportError: &addressingv1.TransportError{Code: code, Message: message, Retryable: retryable},
	})
	_ = msg.Respond(raw)
}

func (registration *Registration) heartbeat(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			record := registration.record
			record.UpdatedAt = time.Now().UTC()
			heartbeatCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := putAnnouncement(heartbeatCtx, registration.router.Directory, registration.key, record)
			cancel()
			if err != nil {
				registration.router.Logf("刷新 capability 租约失败 %s: %v", record.Capability, err)
			}
		}
	}
}

func (registration *Registration) Close(ctx context.Context) error {
	registration.once.Do(func() {
		registration.cancel()
		registration.router.mu.Lock()
		locals := registration.router.local[registration.record.Capability]
		for index := range locals {
			if locals[index].registrationID != registration.id {
				continue
			}
			locals = append(locals[:index], locals[index+1:]...)
			break
		}
		if len(locals) == 0 {
			delete(registration.router.local, registration.record.Capability)
			delete(registration.router.localCursor, registration.record.Capability)
		} else {
			registration.router.local[registration.record.Capability] = locals
		}
		delete(registration.router.registrations, registration.id)
		if instances := registration.router.instances[registration.record.Capability]; instances != nil {
			delete(instances, registration.key)
			if len(instances) == 0 {
				delete(registration.router.instances, registration.record.Capability)
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

func putAnnouncement(ctx context.Context, directory interface {
	Put(context.Context, string, []byte) (uint64, error)
}, key string, record Announcement) error {
	raw, err := json.Marshal(record)
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
