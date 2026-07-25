package protocolbus

import (
	"context"
	"fmt"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

func (h *Host) dispatch(sess *session, msg *pluginhostv1.FromPlugin) {
	switch m := msg.Msg.(type) {
	case *pluginhostv1.FromPlugin_InvokeResult:
		sess.deliver(m.InvokeResult.RequestId, msg)
	case *pluginhostv1.FromPlugin_LifecycleAck:
		sess.deliver(m.LifecycleAck.RequestId, msg)
	case *pluginhostv1.FromPlugin_Pong:
		sess.deliver(m.Pong.RequestId, msg)
	case *pluginhostv1.FromPlugin_HostCall:
		h.dispatchHostCall(sess, m.HostCall)
	case *pluginhostv1.FromPlugin_Event:
		h.dispatchPluginEvent(sess, m.Event)
	case *pluginhostv1.FromPlugin_Register:
		h.updateContributions(sess, m.Register.RequestId, m.Register.Contributions, nil)
	case *pluginhostv1.FromPlugin_Unregister:
		h.updateContributions(sess, m.Unregister.RequestId, nil, m.Unregister.Contributions)
	case *pluginhostv1.FromPlugin_Cancel:
		if sess.hasFeature(protocol.FeatureCancellation) && m.Cancel != nil {
			sess.cancelHostCall(m.Cancel.RequestId)
		}
	default:
		h.Logf("忽略未知消息类型 %T", m)
	}
}

func (h *Host) updateContributions(sess *session, requestID string, additions []*pluginhostv1.Contribution,
	removals []*pluginhostv1.ContributionRef) {
	ack := &pluginhostv1.ContributionUpdateAck{RequestId: requestID, Rejected: map[string]string{}}
	if !sess.hasFeature(protocol.FeatureDynamicContributions) {
		ack.Rejected["*"] = "未协商动态贡献能力"
		h.replyContributionUpdate(sess, ack)
		return
	}
	if requestID == "" {
		ack.Rejected["*"] = "request_id 不能为空"
		h.replyContributionUpdate(sess, ack)
		return
	}
	if len(additions) > 0 {
		if sess.policy.Contributions == nil {
			ack.Rejected["*"] = "动态贡献需要签名清单授权"
			h.replyContributionUpdate(sess, ack)
			return
		}
		if err := validateDeclaredContributions(sess.policy.Contributions, additions, false); err != nil {
			ack.Rejected["*"] = err.Error()
			h.replyContributionUpdate(sess, ack)
			return
		}
		for _, contribution := range additions {
			key := contribution.ExtensionPoint + "/" + contribution.Id
			if err := pluginv1.ValidateDescriptor(contribution.ExtensionPoint, contribution.DescriptorJson); err != nil {
				ack.Rejected[key] = err.Error()
				continue
			}
			if err := h.Registry.Register(registry.Contribution{
				ExtensionPoint: contribution.ExtensionPoint, ID: contribution.Id,
				PluginID: sess.pluginID, Priority: int(contribution.Priority), Descriptor: contribution.DescriptorJson,
			}); err != nil {
				ack.Rejected[key] = err.Error()
				continue
			}
			ack.Accepted = append(ack.Accepted, key)
		}
	}
	for _, contribution := range removals {
		if contribution == nil {
			ack.Rejected["<nil>"] = "贡献引用不能为空"
			continue
		}
		key := contribution.ExtensionPoint + "/" + contribution.Id
		if !authorizedContribution(sess.policy.Contributions, contribution.ExtensionPoint, contribution.Id) {
			ack.Rejected[key] = "贡献未被签名清单授权"
			continue
		}
		if !h.Registry.Unregister(contribution.ExtensionPoint, contribution.Id, sess.pluginID) {
			ack.Rejected[key] = "贡献不存在或不属于当前插件"
			continue
		}
		ack.Accepted = append(ack.Accepted, key)
	}
	h.replyContributionUpdate(sess, ack)
}

func authorizedContribution(expected []pluginv1.RuntimeContribution, extensionPoint, id string) bool {
	for _, contribution := range expected {
		if contribution.ExtensionPoint == extensionPoint && contribution.ID == id {
			return true
		}
	}
	return false
}

func (h *Host) replyContributionUpdate(sess *session, ack *pluginhostv1.ContributionUpdateAck) {
	if err := sess.send(&pluginhostv1.FromHost{Msg: &pluginhostv1.FromHost_ContributionUpdateAck{
		ContributionUpdateAck: ack,
	}}); err != nil {
		h.Logf("回应插件动态贡献更新失败: %v", err)
	}
}

func (h *Host) dispatchPluginEvent(sess *session, envelope *pluginhostv1.EventEnvelope) {
	if !sess.hasFeature(protocol.FeatureEventPublish) || envelope == nil || envelope.Event == nil {
		h.Logf("拒绝插件 %s 的未协商或空事件", sess.pluginID)
		return
	}
	event := proto.Clone(envelope.Event).(*contractv1.CallEvent)
	event.Id = strings.TrimSpace(event.Id)
	event.Type = strings.TrimSpace(event.Type)
	if event.Id == "" || event.Type == "" || len(event.Id) > 160 || len(event.Type) > 160 ||
		!h.limits().PayloadAllowed(event.Payload) {
		h.Logf("拒绝插件 %s 的非法事件", sess.pluginID)
		return
	}
	// source 和 principal_ref 都是权威身份字段，不能信任插件进程自报。
	event.Source = sess.pluginID
	event.PrincipalRef = nil

	h.callbackMu.Lock()
	if h.callbackSlots == nil {
		h.callbackSlots = make(chan struct{}, h.limits().MaxConcurrentCalls)
	}
	slots := h.callbackSlots
	h.callbackMu.Unlock()
	select {
	case slots <- struct{}{}:
		go func() {
			defer func() { <-slots }()
			ctx, cancel := context.WithTimeout(context.Background(), h.callTimeout())
			defer cancel()
			for _, outcome := range h.PublishEvent(ctx, event) {
				if outcome.Err != nil {
					h.Logf("插件事件 %s 投递到 %s 失败: %v", event.Type, outcome.SinkID, outcome.Err)
				}
			}
		}()
	default:
		h.Logf("丢弃插件 %s 事件 %s：宿主回调并发达到上限", sess.pluginID, event.Type)
	}
}

// heartbeat 周期性 Ping；连续无响应即判定失联并摘除其贡献（§2.6 心跳/崩溃）。
func (h *Host) heartbeat(sess *session) {
	every := h.HeartbeatEvery
	if every <= 0 {
		every = defaultHeartbeatEvery
	}
	timeout := h.HeartbeatTimeout
	if timeout <= 0 {
		timeout = defaultHeartbeatTimeout
	}

	ticker := time.NewTicker(every)
	defer ticker.Stop()

	for {
		select {
		case <-sess.done:
			return
		case <-ticker.C:
			if sess.idleFor() > timeout {
				h.Logf("插件 %s 心跳超时（%v 无任何消息），判定失联", sess.pluginID, sess.idleFor())
				h.teardown(sess, fmt.Errorf("心跳超时（%v）", timeout))
				return
			}
			reqID := sess.nextRequestID()
			ch, err := sess.await(reqID, h.limits().MaxPendingRequests)
			if err != nil {
				h.Logf("跳过插件 %s 心跳：%v", sess.pluginID, err)
				continue
			}
			if err := sess.send(&pluginhostv1.FromHost{
				Msg: &pluginhostv1.FromHost_Ping{Ping: &pluginhostv1.Ping{RequestId: reqID}},
			}); err != nil {
				sess.release(reqID)
				return // 发不出去 → 流已断，读循环的 teardown 会处理
			}
			select {
			case <-ch: // 收到 Pong
			case <-time.After(timeout):
				// 不在此直接判死：交给上面的 idleFor 判据，避免与其他消息竞态
			case <-sess.done:
			}
			sess.release(reqID)
		}
	}
}

// teardown 会话终结：摘除其全部贡献、唤醒在途等待者、回收进程（ADR-0004 故障隔离）。
func (h *Host) teardown(sess *session, cause error) {
	sess.teardownOnce.Do(func() {
		defer close(sess.teardownDone)
		sess.autonomousActive.Store(false)
		sess.markDead(cause)

		if n := h.Registry.UnregisterPlugin(sess.pluginID); n > 0 {
			h.Logf("已摘除插件 %s 的 %d 条贡献（原因: %v）", sess.pluginID, n, cause)
		}

		h.mu.Lock()
		delete(h.sessions, sess.id)
		if cur, ok := h.byPlugin[sess.pluginID]; ok && cur == sess {
			delete(h.byPlugin, sess.pluginID)
		}
		h.mu.Unlock()

		h.failLaunch(sess.launchToken, cause) // 若仍在 Launch 等待中，让它立刻脱身
		sess.killProcess()
		// Shared Runtime Hosts own many logical units. Closing one plugin must
		// release only its unit/lease and must never kill the shared process.
		sess.stopManagedUnit()
	})
	// done 只表示流已死亡；teardownDone 才证明贡献、会话表和进程已经全部收敛。
	// Close/Stop 的调用者据此获得同步完成语义，不再与读循环的 defer teardown 竞态。
	<-sess.teardownDone
}
