package versionledger

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (c *Client) PutVersion(ctx context.Context, call *contractv1.CallContext, request versioningv1.PutVersionRequest) (versioningv1.PutVersionResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationPutVersion, request)
	if err != nil {
		return versioningv1.PutVersionResult{}, err
	}
	return *result.(*versioningv1.PutVersionResult), nil
}

func (c *Client) GetVersion(ctx context.Context, call *contractv1.CallContext, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationGetVersion, request)
	if err != nil {
		return versioningv1.GetVersionResult{}, err
	}
	return *result.(*versioningv1.GetVersionResult), nil
}

func (c *Client) ListHistory(ctx context.Context, call *contractv1.CallContext, request versioningv1.ListHistoryRequest) (versioningv1.ListHistoryResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationListHistory, request)
	if err != nil {
		return versioningv1.ListHistoryResult{}, err
	}
	return *result.(*versioningv1.ListHistoryResult), nil
}
