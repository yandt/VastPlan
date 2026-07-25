package plugin

import (
	"context"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
)

func (p *Plugin) Call(ctx context.Context, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	limits := p.Limits.Normalize()
	if target == nil || target.Capability == "" {
		return nil, nil, errors.New("回调宿主的目标 capability 不能为空")
	}
	if !limits.PayloadAllowed(payload) {
		return nil, nil, fmt.Errorf("回调宿主 payload 为 %d bytes，超过上限 %d bytes", len(payload), limits.MaxPayloadBytes)
	}
	if !limits.MetadataAllowed(proto.Size(callCtx)) {
		return nil, nil, fmt.Errorf("回调宿主 CallContext 为 %d bytes，超过 metadata 上限 %d bytes", proto.Size(callCtx), limits.MaxMetadataBytes)
	}
	ctx, callCtx, cancel := pluginCallContext(ctx, callCtx, limits.DefaultDeadline)
	defer cancel()

	reqID := fmt.Sprintf("hc-%d", p.seq.Add(1))
	ch := make(chan *pluginhostv1.FromHost, 1)

	p.pendingMu.Lock()
	if len(p.pending) >= limits.MaxPendingRequests {
		p.pendingMu.Unlock()
		return nil, nil, errors.New("回调宿主 pending 请求队列已满")
	}
	p.pending[reqID] = ch
	p.pendingMu.Unlock()
	defer func() {
		p.pendingMu.Lock()
		delete(p.pending, reqID)
		p.pendingMu.Unlock()
	}()

	if err := p.send(&pluginhostv1.FromPlugin{
		Msg: &pluginhostv1.FromPlugin_HostCall{
			HostCall: &pluginhostv1.InvokeRequest{
				RequestId: reqID, Target: target, Context: callCtx, Payload: payload,
				DelegationToken: delegationTokenFromContext(ctx),
			},
		},
	}); err != nil {
		return nil, nil, fmt.Errorf("回调宿主失败: %w", err)
	}

	select {
	case msg := <-ch:
		r := msg.GetHostCallResult()
		if r == nil {
			return nil, nil, errors.New("宿主回调响应为空")
		}
		if !limits.PayloadAllowed(r.Payload) {
			return nil, nil, fmt.Errorf("宿主回调响应 payload 为 %d bytes，超过上限 %d bytes", len(r.Payload), limits.MaxPayloadBytes)
		}
		return r.Result, r.Payload, nil
	case <-ctx.Done():
		if p.hasFeature(protocol.FeatureCancellation) {
			_ = p.send(&pluginhostv1.FromPlugin{Msg: &pluginhostv1.FromPlugin_Cancel{
				Cancel: &pluginhostv1.Cancel{RequestId: reqID},
			}})
		}
		return nil, nil, ctx.Err()
	}
}

type invocationDelegationKey struct{}

func delegationTokenFromContext(ctx context.Context) *string {
	if ctx == nil {
		return nil
	}
	token, _ := ctx.Value(invocationDelegationKey{}).(string)
	if token == "" {
		return nil
	}
	return &token
}

func (p *Plugin) hasFeature(feature string) bool { return p.features[feature] }

func (p *Plugin) cancelInvoke(requestID string) {
	p.invokeMu.Lock()
	cancel := p.invokeCancels[requestID]
	p.invokeMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// PublishEvent 把插件事件发布到宿主事件总线。宿主会覆盖 source 并清除
// principal_ref；调用方不能借事件字段伪造权威身份。
