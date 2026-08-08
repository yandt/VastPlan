package portalapproval

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type hostPolicy struct {
	client *approvalsdk.Client
	call   *contractv1.CallContext
}

func NewHostPolicy(host sdk.Host, call *contractv1.CallContext, binding approvalv2.ProviderBinding) (Policy, error) {
	if call == nil {
		return nil, errors.New("Approval Provider 调用缺少可信 CallContext")
	}
	client, err := approvalsdk.New(host, binding)
	if err != nil {
		return nil, err
	}
	return hostPolicy{client: client, call: call}, nil
}

func (p hostPolicy) Evaluate(ctx context.Context, input approvalv2.EvaluationInput) (approvalv2.Decision, error) {
	return p.client.Evaluate(ctx, p.call, input)
}

func (p hostPolicy) EvaluateBatch(ctx context.Context, inputs []approvalv2.EvaluationInput) ([]approvalv2.Decision, error) {
	return p.client.EvaluateBatch(ctx, p.call, inputs)
}

func OperationNeedsPolicy(operation string) bool {
	return operation == "portalGovernance" || operation == "approvePortalPublication"
}
