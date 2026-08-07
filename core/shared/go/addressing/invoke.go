package addressing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	addressingv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/addressing/v1"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

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
	eligibleLocals := make([]localHandler, 0, len(locals))
	for _, candidate := range locals {
		if candidate.record.Health != "healthy" ||
			(candidate.record.Readiness != "" && candidate.record.Readiness != "ready" && candidate.record.Readiness != "degraded") ||
			(target.GetLogicalService() != "" && candidate.record.LogicalService != target.GetLogicalService()) ||
			(target.GetRoutingDomain() != "" && candidate.record.RoutingDomain != target.GetRoutingDomain()) ||
			(target.GetPartitionKey() != "" && candidate.record.PartitionKey != target.GetPartitionKey()) ||
			(target.GetInstanceId() != "" && candidate.record.InstanceID != target.GetInstanceId()) {
			continue
		}
		eligibleLocals = append(eligibleLocals, candidate)
	}
	var local localHandler
	if target.GetInstanceId() != "" {
		if len(eligibleLocals) > 0 {
			local = eligibleLocals[0]
		}
	} else if len(eligibleLocals) > 0 {
		cursor := r.localCursor[target.Capability]
		local = eligibleLocals[cursor%uint64(len(eligibleLocals))]
		r.localCursor[target.Capability] = cursor + 1
	}
	r.mu.Unlock()
	if local.handler != nil {
		if r.Transport != nil {
			if err := authorizeCapability(r.Transport.SelfIdentity(), local.record); err != nil {
				return nil, nil, &TransportError{Code: errorcode.PermissionDenied, Message: err.Error()}
			}
		}
		result, out, err := local.handler(ctx, target, callCtx, payload)
		if err == nil && !limits.PayloadAllowed(out) {
			return nil, nil, &TransportError{Code: errorcode.PayloadTooLarge,
				Message: fmt.Sprintf("响应 payload 为 %d bytes，超过上限 %d bytes", len(out), limits.MaxPayloadBytes)}
		}
		return result, out, err
	}
	instances := r.instancesFor(target.Capability, target.GetLogicalService(), target.GetRoutingDomain(), target.GetPartitionKey(), target.GetInstanceId())
	if len(instances) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrCapabilityNotFound, target.Capability)
	}
	subject := instances[0].Subject
	if target.GetInstanceId() != "" {
		selected := instances[0]
		subject = controlplane.RPCInstanceSubject(selected.Capability, selected.LogicalService, selected.RoutingDomain, selected.PartitionKey, selected.InstanceID)
	}
	for _, instance := range instances[1:] {
		if instance.Subject != subject && target.GetLogicalService() == "" && target.GetRoutingDomain() == "" && target.GetPartitionKey() == "" {
			return nil, nil, fmt.Errorf("capability %s 存在多个路由域，调用方必须指定 logical_service/routing_domain", target.Capability)
		}
	}
	requestID := randomID()
	req := &addressingv1.InvokeRequest{RequestId: requestID, Target: target, Context: callCtx, Payload: payload}
	raw, err := proto.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("编码远端调用: %w", err)
	}
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
