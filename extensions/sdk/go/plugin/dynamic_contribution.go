package plugin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"google.golang.org/protobuf/proto"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
)

func (p *Plugin) PublishEvent(event *contractv1.CallEvent) error {
	if !p.hasFeature(protocol.FeatureEventPublish) {
		return errors.New("宿主未协商插件事件发布能力")
	}
	if event == nil || event.Id == "" || event.Type == "" {
		return errors.New("事件 id 和 type 不能为空")
	}
	if !p.Limits.Normalize().PayloadAllowed(event.Payload) {
		return errors.New("事件 payload 超过协议上限")
	}
	return p.send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Event{
		Event: &pluginhostv1.EventEnvelope{Event: event},
	}})
}

// RegisterContribution 在运行期启用一条已被签名清单声明的贡献。
func (p *Plugin) RegisterContribution(ctx context.Context, contribution Contribution) error {
	// 先安装本地路由，再让宿主暴露路由，避免宿主 ACK 后的首个调用撞上空窗口。
	p.installLocalContribution(contribution)
	ack, err := p.updateContribution(ctx, &pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Register{
		Register: &pluginhostv1.RegisterContributions{Contributions: []*pluginhostv1.Contribution{wireContribution(contribution)}},
	}})
	if err != nil {
		p.removeLocalContribution(contribution.ExtensionPoint, contribution.ID)
		return err
	}
	if len(ack.Rejected) > 0 {
		p.removeLocalContribution(contribution.ExtensionPoint, contribution.ID)
		return fmt.Errorf("宿主拒绝动态贡献: %v", ack.Rejected)
	}
	return nil
}

// UnregisterContribution 在运行期停用当前插件拥有的贡献。
func (p *Plugin) UnregisterContribution(ctx context.Context, extensionPoint, id string) error {
	ack, err := p.updateContribution(ctx, &pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Unregister{
		Unregister: &pluginhostv1.UnregisterContributions{Contributions: []*pluginhostv1.ContributionRef{{
			ExtensionPoint: extensionPoint, Id: id,
		}}},
	}})
	if err != nil {
		return err
	}
	if len(ack.Rejected) > 0 {
		return fmt.Errorf("宿主拒绝动态卸载贡献: %v", ack.Rejected)
	}
	p.removeLocalContribution(extensionPoint, id)
	return nil
}

func (p *Plugin) installLocalContribution(contribution Contribution) {
	p.contribMu.Lock()
	defer p.contribMu.Unlock()
	p.contribs = append(p.contribs, contribution)
	for operation, handler := range contribution.Handlers {
		p.routes[routeKey(contribution.ExtensionPoint, contribution.ID, operation)] = handler
	}
}

func (p *Plugin) removeLocalContribution(extensionPoint, id string) {
	p.contribMu.Lock()
	defer p.contribMu.Unlock()
	filtered := p.contribs[:0]
	for _, contribution := range p.contribs {
		if contribution.ExtensionPoint != extensionPoint || contribution.ID != id {
			filtered = append(filtered, contribution)
		}
	}
	p.contribs = filtered
	prefix := extensionPoint + "|" + id + "|"
	for key := range p.routes {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(p.routes, key)
		}
	}
}

func (p *Plugin) updateContribution(ctx context.Context, message *pluginhostv1.FromPlugin) (*pluginhostv1.ContributionUpdateAck, error) {
	if !p.hasFeature(protocol.FeatureDynamicContributions) {
		return nil, errors.New("宿主未协商动态贡献能力")
	}
	reqID := fmt.Sprintf("cu-%d", p.seq.Add(1))
	switch typed := message.Msg.(type) {
	case *pluginhostv1.FromPlugin_Register:
		typed.Register.RequestId = reqID
	case *pluginhostv1.FromPlugin_Unregister:
		typed.Unregister.RequestId = reqID
	}
	ch := make(chan *pluginhostv1.FromHost, 1)
	p.pendingMu.Lock()
	if len(p.pending) >= p.Limits.Normalize().MaxPendingRequests {
		p.pendingMu.Unlock()
		return nil, errors.New("动态贡献 pending 请求队列已满")
	}
	p.pending[reqID] = ch
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, reqID)
		p.pendingMu.Unlock()
	}()
	if err := p.send(message); err != nil {
		return nil, err
	}
	select {
	case response := <-ch:
		ack := response.GetContributionUpdateAck()
		if ack == nil {
			return nil, errors.New("动态贡献响应为空")
		}
		return ack, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func wireContribution(c Contribution) *pluginhostv1.Contribution {
	return &pluginhostv1.Contribution{ExtensionPoint: c.ExtensionPoint, Id: c.ID,
		Priority: c.Priority, DescriptorJson: c.Descriptor}
}

func pluginCallContext(ctx context.Context, callCtx *contractv1.CallContext, timeout time.Duration) (context.Context, *contractv1.CallContext, context.CancelFunc) {
	deadline := time.Now().Add(timeout)
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
	if inherited, ok := ctx.Value(invocationCallPathKey{}).([]string); ok {
		bounded.CallPath = append([]string(nil), inherited...)
	}
	deadlineUnixMs := deadline.UnixMilli()
	bounded.DeadlineUnixMs = &deadlineUnixMs
	boundedCtx, cancel := context.WithDeadline(ctx, deadline)
	return boundedCtx, bounded, cancel
}

func (p *Plugin) deliver(reqID string, msg *pluginhostv1.FromHost) {
	p.pendingMu.Lock()
	ch, ok := p.pending[reqID]
	p.pendingMu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- msg:
	default:
	}
}

func errResult(code, msg string, retryable bool) *pluginhostv1.InvokeResponse {
	return &pluginhostv1.InvokeResponse{
		Result: &contractv1.CallResult{
			Status: contractv1.CallResult_STATUS_ERROR,
			Error:  &contractv1.Error{Code: code, Message: msg, Retryable: retryable},
		},
	}
}

// OK 构造一个成功的 CallResult（便利函数）。
func OK(durationMs int64) *contractv1.CallResult {
	return &contractv1.CallResult{
		Status: contractv1.CallResult_STATUS_OK,
		Usage:  &contractv1.Usage{DurationMs: durationMs},
	}
}
