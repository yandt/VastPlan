package versionworkspace

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func (c *Client) DescribeResource(ctx context.Context, call *contractv1.CallContext, request workspacev1.DescribeResourceRequest) (workspacev1.ResourceDescription, error) {
	result, err := c.call(ctx, call, workspacev1.OperationDescribeResource, request)
	if err != nil {
		return workspacev1.ResourceDescription{}, err
	}
	return *result.(*workspacev1.ResourceDescription), nil
}
