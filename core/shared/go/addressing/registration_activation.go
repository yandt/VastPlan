package addressing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/addressing/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
)

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
		if registration.localOnly {
			record.Health, record.Readiness = "healthy", "ready"
			record.UpdatedAt = time.Now().UTC()
			registration.record = record
			activated = append(activated, registration)
			continue
		}
		record.Health = "healthy"
		if record.Readiness == "" {
			record.Readiness = "ready"
		}
		record.UpdatedAt = time.Now().UTC()
		record.LeaseExpiresAt = record.UpdatedAt.Add(30 * time.Second)
		if err := router.putAnnouncement(ctx, router.Directory, registration.key, record); err != nil {
			for _, previous := range activated {
				if previous.localOnly {
					continue
				}
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
			if registration.localOnly {
				continue
			}
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
			registrationID: registration.id, handler: registration.handler, record: registration.record,
		})
		registration.active.Store(true)
	}
	router.notifyTopologyChangeLocked()
	router.mu.Unlock()
	return nil
}

func (r *Router) serveInvoke(registrationID string, record Announcement, handler InvokeHandler, msg *nats.Msg) {
	if msg.Reply == "" {
		return
	}
	limits := r.Limits.Normalize()
	if len(msg.Data) > limits.MaxMessageBytes() {
		r.respondTransportError(msg, errorcode.PayloadTooLarge, "请求信封超过协议消息上限", false, "")
		return
	}
	var identity TransportIdentity
	var transportTrusted *callcontext.Trusted
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
	if req.Target == nil || req.Target.Capability != record.Capability ||
		(req.Target.GetInstanceId() != "" && req.Target.GetInstanceId() != record.InstanceID) {
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
		if err := authorizeCapability(identity, record); err != nil {
			r.respondTransportError(msg, errorcode.PermissionDenied, err.Error(), false, req.RequestId)
			return
		}
		authenticated, err := authenticatedTransportTrustedContext(identity, req.Context)
		if err != nil {
			r.respondTransportError(msg, errorcode.PermissionDenied, err.Error(), false, req.RequestId)
			return
		}
		req.Context = authenticated.Wire()
		transportTrusted = &authenticated
	}
	handlerCtx, boundedCallCtx, cancel := r.boundedCallContext(r.ctx, req.Context)
	if transportTrusted != nil {
		handlerCtx = callcontext.WithTrusted(handlerCtx, *transportTrusted)
	}
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
		r.Logf("回应 capability %s 失败: %v", record.Capability, err)
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
