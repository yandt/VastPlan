package hostfactory

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func decodeStrict(payload []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("请求只能包含一个 JSON 文档")
	}
	return nil
}

func kernelNodeReadiness(observer nodebootstrap.ReadinessObserver) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != nodebootstrap.DeploymentManagerPluginID {
			return nil, nil, errors.New("kernel.node.readiness 只接受 deployment-manager 认证会话")
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var expectation nodebootstrap.ReadinessExpectation
		if err := decoder.Decode(&expectation); err != nil {
			return nil, nil, errors.New("节点就绪期望无效")
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("节点就绪期望只能包含一个 JSON 文档")
		}
		if err := expectation.Validate(); err != nil || expectation.TenantID != callCtx.GetTenantId() {
			return nil, nil, errors.New("节点就绪期望与认证租户不匹配")
		}
		observation, err := observer.Observe(ctx, expectation)
		if err != nil {
			return nil, nil, errors.New("可信节点就绪观察失败")
		}
		raw, err := json.Marshal(observation)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func kernelNodeBootstrap(broker nodebootstrap.Broker) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != nodebootstrap.DeploymentManagerPluginID {
			return nil, nil, fmt.Errorf("kernel.node.bootstrap 只接受 deployment-manager 认证会话")
		}
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		var request nodebootstrap.ExecutionRequest
		if err := decoder.Decode(&request); err != nil {
			return nil, nil, fmt.Errorf("节点引导计划无效: %w", err)
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			return nil, nil, errors.New("节点引导计划只能包含一个 JSON 文档")
		}
		if err := request.Validate(); err != nil || request.Plan.Node.Tenant != callCtx.GetTenantId() {
			return nil, nil, errors.New("节点引导计划与认证租户不匹配")
		}
		fence, err := deploymentManagerFence(ctx, callCtx, "node-bootstrap/"+request.OperationID)
		if err != nil {
			return nil, nil, err
		}
		scope := nodebootstrap.Scope{TenantID: callCtx.GetTenantId(), ProjectID: callCtx.GetProjectId(), PluginID: callCtx.GetCaller().GetId()}
		if err := scope.Validate(); err != nil {
			return nil, nil, err
		}
		result, err := broker.Bootstrap(ctx, scope, fence, request.Plan)
		if err != nil {
			return nil, nil, errors.New("可信节点引导执行失败")
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

type configGetRequest struct {
	Key string `json:"key"`
}

type managedCredentialRefRequest struct {
	FieldID string `json:"fieldId"`
}

func kernelManagedCredentialRef(provider kernelspi.ManagedCredentialRefProvider) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, fmt.Errorf("kernel.config.credential-ref 只接受已认证插件会话")
		}
		var request managedCredentialRefRequest
		if err := json.Unmarshal(payload, &request); err != nil || strings.TrimSpace(request.FieldID) == "" {
			return nil, nil, fmt.Errorf("托管凭证字段不能为空")
		}
		ref, ok, err := provider.LookupManagedCredential(ctx, callCtx.GetCaller().GetId(), request.FieldID)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, kernelspi.ErrNotFound
		}
		raw, err := json.Marshal(ref)
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
	}
}

func kernelConfigGet(provider kernelspi.ConfigProvider) protocolbus.HostService {
	return func(ctx context.Context, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() == "" {
			return nil, nil, fmt.Errorf("kernel.config.get 只接受已认证插件会话")
		}
		var request configGetRequest
		if err := json.Unmarshal(payload, &request); err != nil || request.Key == "" {
			return nil, nil, fmt.Errorf("配置请求 key 不能为空")
		}
		value, ok, err := provider.Lookup(ctx, callCtx.GetCaller().GetId(), request.Key)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			return nil, nil, kernelspi.ErrNotFound
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, value, nil
	}
}

func kernelDiagnostics(host *protocolbus.Host) protocolbus.HostService {
	return func(_ context.Context, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		out, err := json.Marshal(host.DiagnosticSnapshot())
		if err != nil {
			return nil, nil, err
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
	}
}

func kernelInfo(version string) protocolbus.HostService {
	return func(_ context.Context, callCtx *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
		out, _ := json.Marshal(map[string]any{
			"kernel":     KernelName,
			"version":    version,
			"callerKind": callCtx.GetCaller().GetKind().String(),
			"tenant":     callCtx.GetTenantId(),
		})
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, out, nil
	}
}
