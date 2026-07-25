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
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

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
		eventCtx := r.ctx
		if r.Transport != nil {
			authenticated, err := authenticatedTransportTrustedContext(identity, envelope.Context)
			if err != nil {
				r.Logf("丢弃租户身份不一致的事件 subject=%s: %v", msg.Subject, err)
				return
			}
			envelope.Context = authenticated.Wire()
			eventCtx = callcontext.WithTrusted(eventCtx, authenticated)
		}
		if err := handler(eventCtx, envelope.Context, envelope.Event); err != nil {
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

// LocalInstances 返回当前内核中已激活的本地 capability 元数据。它供 same-node /
// same-kernel 依赖校验版本与 readiness 使用，但不会把 local capability 泄漏到全局目录。
func (r *Router) LocalInstances(capability string) []Announcement {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := r.local[capability]
	out := make([]Announcement, 0, len(entries))
	for _, entry := range entries {
		if entry.record.Health == "healthy" && (entry.record.Readiness == "" || entry.record.Readiness == "ready" || entry.record.Readiness == "degraded") {
			out = append(out, entry.record)
		}
	}
	return out
}

// InstancesFor 只返回指定逻辑服务和路由域中的健康实例。空过滤条件保持 v1 兼容行为。
func (r *Router) InstancesFor(capability, logicalService, routingDomain string) []Announcement {
	return r.instancesFor(capability, logicalService, routingDomain, "", "")
}

func (r *Router) instancesFor(capability, logicalService, routingDomain, partitionKey, instanceID string) []Announcement {
	r.mu.RLock()
	defer r.mu.RUnlock()
	entries := r.instances[capability]
	out := make([]Announcement, 0, len(entries))
	for _, entry := range entries {
		if entry.Health == "healthy" && (logicalService == "" || entry.LogicalService == logicalService) &&
			(routingDomain == "" || entry.RoutingDomain == routingDomain) &&
			(partitionKey == "" || entry.PartitionKey == partitionKey) &&
			(instanceID == "" || entry.InstanceID == instanceID) &&
			(entry.Readiness == "" || entry.Readiness == "ready" || entry.Readiness == "degraded") {
			if r.Transport != nil && authorizeCapability(r.Transport.SelfIdentity(), entry) != nil {
				continue
			}
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
	if identity.NodeID != record.NodeID {
		return fmt.Errorf("能力目录签名身份 node_id %q 与公告 node_id %q 不一致", identity.NodeID, record.NodeID)
	}
	return nil
}
