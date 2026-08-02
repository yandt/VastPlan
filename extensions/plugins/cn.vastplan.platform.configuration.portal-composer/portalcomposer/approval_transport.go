package portalcomposer

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type hostApprovalPolicy struct {
	client  *approvalsdk.Client
	callCtx *contractv1.CallContext
}

func newHostApprovalPolicy(host sdk.Host, callCtx *contractv1.CallContext, binding approvalv2.ProviderBinding) (ApprovalPolicy, error) {
	if callCtx == nil {
		return nil, errors.New("Approval Provider 调用缺少可信 CallContext")
	}
	client, err := approvalsdk.New(host, binding)
	if err != nil {
		return nil, err
	}
	return hostApprovalPolicy{client: client, callCtx: callCtx}, nil
}

func (p hostApprovalPolicy) Evaluate(ctx context.Context, input approvalv2.EvaluationInput) (approvalv2.Decision, error) {
	return p.client.Evaluate(ctx, p.callCtx, input)
}

func (p hostApprovalPolicy) EvaluateBatch(ctx context.Context, inputs []approvalv2.EvaluationInput) ([]approvalv2.Decision, error) {
	return p.client.EvaluateBatch(ctx, p.callCtx, inputs)
}

func operationNeedsApprovalPolicy(operation string) bool {
	return operation == "portalGovernance" || operation == "approvePortalPublication"
}
