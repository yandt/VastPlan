package main

import (
	"context"
	"log"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	policy "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.authorization-policy/authorizationpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type leaseLifecycle struct {
	service *policy.Service
	host    sdk.Host
	call    *contractv1.CallContext
	mu      sync.Mutex
	cancel  context.CancelFunc
}

func newLeaseLifecycle(service *policy.Service, host sdk.Host, tenantID string) *leaseLifecycle {
	return &leaseLifecycle{service: service, host: host, call: &contractv1.CallContext{TenantId: tenantID}}
}

func (l *leaseLifecycle) Handle(_ context.Context, lifecycle *pluginhostv1.Lifecycle) error {
	switch lifecycle.GetOp() {
	case pluginhostv1.Lifecycle_OP_ACTIVATE:
		l.stop()
		controllerCtx, cancel := context.WithCancel(context.Background())
		l.mu.Lock()
		l.cancel = cancel
		l.mu.Unlock()
		// ACTIVATE 返回后宿主才授予后台调用身份与 Leader fence。控制器稍后
		// 立即协调；fence 尚未可用时按统一重试语义收敛，不绕过门禁。
		go l.service.RunSnapshotLeaseController(controllerCtx, l.host, l.call, time.Now().Add(250*time.Millisecond), func(err error) {
			log.Printf("Authorization Snapshot Lease 协调失败，将重试: %v", err)
		})
	case pluginhostv1.Lifecycle_OP_DEACTIVATE, pluginhostv1.Lifecycle_OP_DRAIN, pluginhostv1.Lifecycle_OP_SHUTDOWN:
		l.stop()
	}
	return nil
}

func (l *leaseLifecycle) stop() {
	l.mu.Lock()
	cancel := l.cancel
	l.cancel = nil
	l.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}
