package main

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const sharedStateNamespace = "platform.database.connections.v3"
const sharedStateKey = "state"

type sharedStateOperation struct {
	ctx      context.Context
	call     *contractv1.CallContext
	client   *sharedstatesdk.Client
	revision uint64
}

type stateProtocol interface {
	begin(*service, context.Context, sdk.Host, *contractv1.CallContext, string) error
	end(*service)
	save([]byte) error
}

type sharedStateProtocol struct{ operation *sharedStateOperation }

func (s *service) beginStateOperation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string) error {
	if s.state == nil {
		return errors.New("数据库状态协议未配置")
	}
	return s.state.begin(s, ctx, host, call, tenantID)
}

func (p *sharedStateProtocol) begin(s *service, ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenantID string) error {
	client, err := sharedstatesdk.NewFenced(host, "tenant", sharedStateNamespace)
	if err != nil {
		return err
	}
	operation := &sharedStateOperation{ctx: ctx, call: call, client: client}
	entry, err := client.Get(ctx, call, sharedStateKey)
	if err != nil && !sharedstatesdk.IsNotFound(err) {
		return err
	}
	data := emptyPersisted()
	if err == nil {
		if err := decodePersisted(entry.Value, &data); err != nil {
			return err
		}
		operation.revision = entry.Revision
	}
	if err := requireSingleTenantState(data, tenantID); err != nil {
		return err
	}
	s.data = data
	p.operation = operation
	return nil
}

func (s *service) endStateOperation() {
	if s.state != nil {
		s.state.end(s)
	}
}

func (p *sharedStateProtocol) end(s *service) {
	p.operation = nil
	s.data = emptyPersisted()
}

func (p *sharedStateProtocol) save(raw []byte) error {
	if p.operation == nil {
		return errors.New("数据库 Shared State 保存缺少当前操作")
	}
	return p.operation.save(raw)
}

func (o *sharedStateOperation) save(raw []byte) error {
	var (
		entry sharedstatesdk.Entry
		err   error
	)
	if o.revision == 0 {
		entry, err = o.client.Create(o.ctx, o.call, sharedStateKey, raw)
	} else {
		entry, err = o.client.Update(o.ctx, o.call, sharedStateKey, raw, o.revision)
	}
	if err != nil {
		return err
	}
	o.revision = entry.Revision
	return nil
}

func requireSingleTenantState(data persisted, tenantID string) error {
	if tenantID == "" {
		return errors.New("数据库 Shared State 缺少 tenant")
	}
	if containsOtherTenant(data.Tenants, tenantID) || containsOtherTenant(data.Revisions, tenantID) ||
		containsOtherTenant(data.Pending, tenantID) || containsOtherTenant(data.Publications, tenantID) ||
		containsOtherTenant(data.Retire, tenantID) || containsOtherTenant(data.TestCleanup, tenantID) {
		return errors.New("数据库 Shared State 包含跨租户状态")
	}
	return nil
}

func containsOtherTenant[T any](values map[string]T, tenantID string) bool {
	for key := range values {
		if key != tenantID {
			return true
		}
	}
	return false
}
