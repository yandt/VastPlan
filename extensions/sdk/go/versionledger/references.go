package versionledger

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
)

func (c *Client) GetHead(ctx context.Context, call *contractv1.CallContext, request versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationGetHead, request)
	if err != nil {
		return versioningv1.GetHeadResult{}, err
	}
	return *result.(*versioningv1.GetHeadResult), nil
}

func (c *Client) CreateHead(ctx context.Context, call *contractv1.CallContext, request versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationCreateHead, request)
	if err != nil {
		return versioningv1.CreateHeadResult{}, err
	}
	return *result.(*versioningv1.CreateHeadResult), nil
}

func (c *Client) MoveHead(ctx context.Context, call *contractv1.CallContext, request versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationMoveHead, request)
	if err != nil {
		return versioningv1.MoveHeadResult{}, err
	}
	return *result.(*versioningv1.MoveHeadResult), nil
}

func (c *Client) CreateTag(ctx context.Context, call *contractv1.CallContext, request versioningv1.CreateTagRequest) (versioningv1.CreateTagResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationCreateTag, request)
	if err != nil {
		return versioningv1.CreateTagResult{}, err
	}
	return *result.(*versioningv1.CreateTagResult), nil
}

func (c *Client) CompareVersions(ctx context.Context, call *contractv1.CallContext, request versioningv1.CompareVersionsRequest) (versioningv1.CompareVersionsResult, error) {
	result, err := c.call(ctx, call, versioningv1.OperationCompare, request)
	if err != nil {
		return versioningv1.CompareVersionsResult{}, err
	}
	return *result.(*versioningv1.CompareVersionsResult), nil
}
