package interaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/schemas/ui/v1"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
)

// Invoker is satisfied by the local protocol host and the remote addressing
// router. Runner profiles can choose either without changing workflow code.
type Invoker interface {
	Invoke(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

type ProtocolBroker struct{ invoker Invoker }

func NewProtocolBroker(invoker Invoker) (*ProtocolBroker, error) {
	if invoker == nil {
		return nil, errors.New("Runner Broker transport 不能为空")
	}
	return &ProtocolBroker{invoker: invoker}, nil
}

func (b *ProtocolBroker) Open(ctx context.Context, source interactionapi.Subject, request uiv1.InteractionRequest) (interactionapi.Record, error) {
	return invoke[interactionapi.Record](ctx, b.invoker, source, "open", request)
}
func (b *ProtocolBroker) Watch(ctx context.Context, source interactionapi.Subject, id string, after time.Time) (interactionapi.Record, error) {
	return invoke[interactionapi.Record](ctx, b.invoker, source, "watch", struct {
		ID    string    `json:"id"`
		After time.Time `json:"after"`
	}{id, after})
}
func (b *ProtocolBroker) Cancel(ctx context.Context, source interactionapi.Subject, id string) (interactionapi.Record, error) {
	return invoke[interactionapi.Record](ctx, b.invoker, source, "cancel", struct {
		ID string `json:"id"`
	}{id})
}

func invoke[T any](ctx context.Context, invoker Invoker, source interactionapi.Subject, operation string, request any) (T, error) {
	var zero T
	if source.ID == "" || source.TenantID == "" {
		return zero, errors.New("Runner Broker 调用缺少来源或 tenant")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return zero, err
	}
	op := operation
	result, raw, err := invoker.Invoke(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: interactionapi.Capability, Operation: &op}, &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_RUNNER, Id: source.ID}, TenantId: source.TenantID, Scene: "runner.interaction"}, payload)
	if err != nil {
		return zero, fmt.Errorf("调用 Interaction Broker: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return zero, errors.New("Interaction Broker 拒绝调用")
	}
	var value T
	if err := json.Unmarshal(raw, &value); err != nil {
		return zero, fmt.Errorf("解析 Interaction Broker 响应: %w", err)
	}
	return value, nil
}
