package main

import (
	"context"
	"fmt"
	"time"

	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/nodeagent"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/recoverycontroller"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicewatchdog"
)

type reconcileRecovery struct {
	controller *recoverycontroller.Controller
	server     *recoverycontroller.Server
}

func buildReconcileRecovery(options reconcileOptions, prepared reconcilePreparation, plane *nodeControlPlane, notifier *servicewatchdog.Notifier, logf func(string, ...any)) (*reconcileRecovery, error) {
	if prepared.capsule == nil {
		return nil, nil
	}
	tenant, deployment := controlPlaneScope(options.deploymentTenant, options.deploymentName)
	controller, err := recoverycontroller.New(*prepared.capsule, options.nodeID, tenant, deployment, options.recoveryStatus)
	if err != nil {
		return nil, err
	}
	controller.Nodes = plane.buckets.Nodes
	if plane.transport != nil {
		controller.Verify = func(record controlplane.NodeRecord) error {
			_, err := plane.transport.VerifyNodeLease(record)
			return err
		}
	}
	controller.Notify = func(status recoveryv1.Status) {
		if err := notifier.Status(fmt.Sprintf("Recovery Capsule %s nodes=%d", status.Overall, status.Nodes)); err != nil {
			logf("systemd Recovery 状态通知失败: %v", err)
		}
	}
	return &reconcileRecovery{controller: controller}, nil
}

func (r *reconcileRecovery) start(options reconcileOptions, lease *nodeLeaseGuard, stateStore nodeagent.StateStore) error {
	if r == nil || r.controller == nil {
		return nil
	}
	if lease != nil && lease.lease != nil {
		if err := r.controller.AttachLease(lease.lease); err != nil {
			return err
		}
	}
	actual, err := stateStore.Load()
	if err != nil {
		return err
	}
	if err := r.observe(actual); err != nil {
		return err
	}
	if options.recoveryListen != "" {
		r.server, err = recoverycontroller.StartServer(options.recoveryListen, r.controller)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *reconcileRecovery) close() error {
	if r == nil || r.server == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.server.Close(ctx)
}
