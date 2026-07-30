package versionworkspace

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versioningv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versioning/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	ledgerclient "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionledger"
)

type hostLedger struct {
	client *ledgerclient.Client
	call   *contractv1.CallContext
}

func newHostLedger(host sdk.Host, call *contractv1.CallContext) (*hostLedger, error) {
	client, err := ledgerclient.New(host)
	if err != nil {
		return nil, err
	}
	return &hostLedger{client: client, call: call}, nil
}

func (l *hostLedger) PutVersion(ctx context.Context, request versioningv1.PutVersionRequest) (versioningv1.PutVersionResult, error) {
	result, err := l.client.PutVersion(ctx, l.call, request)
	return result, translateLedgerClientError(err)
}

func (l *hostLedger) GetVersion(ctx context.Context, request versioningv1.GetVersionRequest) (versioningv1.GetVersionResult, error) {
	result, err := l.client.GetVersion(ctx, l.call, request)
	return result, translateLedgerClientError(err)
}

func (l *hostLedger) GetHead(ctx context.Context, request versioningv1.GetHeadRequest) (versioningv1.GetHeadResult, error) {
	result, err := l.client.GetHead(ctx, l.call, request)
	return result, translateLedgerClientError(err)
}

func (l *hostLedger) CreateHead(ctx context.Context, request versioningv1.CreateHeadRequest) (versioningv1.CreateHeadResult, error) {
	result, err := l.client.CreateHead(ctx, l.call, request)
	return result, translateLedgerClientError(err)
}

func (l *hostLedger) MoveHead(ctx context.Context, request versioningv1.MoveHeadRequest) (versioningv1.MoveHeadResult, error) {
	result, err := l.client.MoveHead(ctx, l.call, request)
	return result, translateLedgerClientError(err)
}

func translateLedgerClientError(err error) error {
	if err == nil {
		return nil
	}
	var serviceErr *ledgerclient.ServiceError
	if errors.As(err, &serviceErr) {
		return ledgerError(serviceErr.Code, serviceErr.Retryable, err)
	}
	return ledgerError(versioningv1.ErrorProviderUnavailable, true, err)
}
