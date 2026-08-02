// Package approvalpolicy is the Go client for approval.policy.v2 providers.
// It owns only protocol validation and capability routing; policy evaluation
// remains inside the selected Provider plugin.
package approvalpolicy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type Client struct {
	host    sdk.Host
	binding approvalv2.ProviderBinding
}

type ServiceError struct {
	Code      string
	Message   string
	Retryable bool
}

func (e *ServiceError) Error() string {
	if e == nil {
		return "Approval Provider 调用失败"
	}
	return e.Code + ": " + e.Message
}

func New(host sdk.Host, binding approvalv2.ProviderBinding) (*Client, error) {
	if host == nil {
		return nil, errors.New("Approval Provider client 缺少宿主")
	}
	if err := approvalv2.ValidateBinding(binding); err != nil {
		return nil, err
	}
	return &Client{host: host, binding: binding}, nil
}

func (c *Client) Evaluate(ctx context.Context, call *contractv1.CallContext, input approvalv2.EvaluationInput) (approvalv2.Decision, error) {
	if err := approvalv2.ValidateInput(input); err != nil {
		return approvalv2.Decision{}, err
	}
	if err := validateTrustedInput(call, input); err != nil {
		return approvalv2.Decision{}, err
	}
	result, err := c.call(ctx, call, "evaluate", approvalv2.EvaluateRequest{Profile: c.binding.Profile, Input: input})
	if err != nil {
		return approvalv2.Decision{}, err
	}
	var response approvalv2.EvaluateResult
	if err := approvalv2.DecodeStrict(result, &response); err != nil {
		return approvalv2.Decision{}, fmt.Errorf("Approval Provider 返回无效 evaluate 结果: %w", err)
	}
	if err := c.validateDecision(response.Decision); err != nil {
		return approvalv2.Decision{}, err
	}
	return response.Decision, nil
}

func (c *Client) EvaluateBatch(ctx context.Context, call *contractv1.CallContext, inputs []approvalv2.EvaluationInput) ([]approvalv2.Decision, error) {
	if len(inputs) == 0 || len(inputs) > 256 {
		return nil, errors.New("Approval Provider batch 数量必须为 1 至 256")
	}
	for _, input := range inputs {
		if err := approvalv2.ValidateInput(input); err != nil {
			return nil, err
		}
		if err := validateTrustedInput(call, input); err != nil {
			return nil, err
		}
	}
	result, err := c.call(ctx, call, "evaluateBatch", approvalv2.EvaluateBatchRequest{Profile: c.binding.Profile, Inputs: inputs})
	if err != nil {
		return nil, err
	}
	var response approvalv2.EvaluateBatchResult
	if err := approvalv2.DecodeStrict(result, &response); err != nil || len(response.Decisions) != len(inputs) {
		return nil, errors.New("Approval Provider 返回的 batch 决定数量或格式无效")
	}
	for _, decision := range response.Decisions {
		if err := c.validateDecision(decision); err != nil {
			return nil, err
		}
	}
	return response.Decisions, nil
}

func (c *Client) call(ctx context.Context, call *contractv1.CallContext, operation string, request any) ([]byte, error) {
	if c == nil || c.host == nil || call == nil || call.Principal == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return nil, errors.New("Approval Provider 调用上下文不完整")
	}
	if call.Principal.UserId == "" {
		return nil, errors.New("Approval Provider 调用缺少可信主体")
	}
	raw, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	logicalService, routingDomain := c.binding.LogicalService, c.binding.RoutingDomain
	result, response, err := c.host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: c.binding.Capability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, raw)
	if err != nil {
		return nil, fmt.Errorf("调用 Approval Provider %s: %w", operation, err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		serviceErr := &ServiceError{Code: "approval.provider.unavailable", Message: "Approval Provider 拒绝调用", Retryable: true}
		if result != nil && result.Error != nil {
			serviceErr.Code, serviceErr.Message, serviceErr.Retryable = result.Error.Code, result.Error.Message, result.Error.Retryable
		}
		return nil, serviceErr
	}
	return response, nil
}

func (c *Client) validateDecision(value approvalv2.Decision) error {
	if value.Profile != c.binding.Profile {
		return errors.New("Approval Provider 返回了 Binding 之外的 Policy Profile")
	}
	if err := approvalv2.ValidateDecision(value); err != nil {
		return fmt.Errorf("Approval Provider 返回了无效决定: %w", err)
	}
	return nil
}

func validateTrustedInput(call *contractv1.CallContext, input approvalv2.EvaluationInput) error {
	if call == nil || call.Principal == nil || strings.TrimSpace(call.Principal.UserId) == "" || strings.TrimSpace(call.GetTenantId()) == "" {
		return errors.New("Approval Provider 调用上下文不完整")
	}
	if input.Actor.ID != call.Principal.UserId || input.TenantID != call.GetTenantId() {
		return errors.New("Approval facts 与可信 CallContext 主体或租户不一致")
	}
	return nil
}
