package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
)

// Reconciler 把一份完整期望态收敛到当前节点。
type Reconciler struct {
	NodeID                    string
	NodeLabels                map[string]string
	Sources                   []ArtifactSource
	Verifier                  ArtifactVerifier
	Installer                 Installer
	Runtime                   Runtime
	StateStore                StateStore
	References                ArtifactReferencePublisher
	BootstrapInventory        *bootstrapinventory.Inventory
	BootstrapReferences       ArtifactReferencePublisher
	BootstrapUpgrade          BootstrapUpgradeCoordinator
	RequireArtifactReferences bool
	Now                       func() time.Time
	// Pulse marks progress through potentially long multi-unit reconciliation.
	// It is deliberately host-only and never enters DesiredState.
	Pulse func()
	// StateObserver receives each durably saved checkpoint. It is a kernel-only
	// projection hook for recovery/readiness and must not mutate desired state.
	StateObserver func(ActualState) error
}

func (r *Reconciler) pulse() {
	if r != nil && r.Pulse != nil {
		r.Pulse()
	}
}

// Reconcile 每次执行都是幂等的。当前实例与候选实例分别持久化；候选插件全部安装且
// 启动成功后 Runtime 才替换旧实例，任一阶段失败都保留旧实例并留下候选失败实际态。
func (r *Reconciler) Reconcile(ctx context.Context, desired deploymentv1.DesiredState) (Result, error) {
	r.pulse()
	actual, err := r.beginReconcile(desired)
	if err != nil {
		return Result{}, err
	}
	targets := r.targetUnits(desired)
	changed, err := r.reconcileTargets(ctx, desired.Revision, targets, &actual)
	if err != nil {
		return Result{Changed: changed, State: actual}, err
	}
	removed, err := r.removeObsoleteUnits(ctx, targets, &actual)
	changed = changed || removed
	if err != nil {
		return Result{Changed: changed, State: actual}, err
	}

	converged := r.isConverged(targets, actual)
	if err := r.checkpoint(&actual); err != nil {
		return Result{}, err
	}
	if converged {
		if err := r.publishAssignmentReferences(ctx, desired.Revision, &actual, false); err != nil {
			actual.Errors = append(actual.Errors, OperationError{Stage: "artifact_reference", Message: err.Error()})
			_ = r.checkpoint(&actual)
			return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("发布 Assignment 制品引用失败: %w", err)
		}
		if r.BootstrapUpgrade != nil {
			inventory, err := r.BootstrapUpgrade.Commit(ctx)
			if err != nil {
				actual.Errors = append(actual.Errors, OperationError{Stage: "bootstrap_upgrade_commit", Message: err.Error()})
				_ = r.checkpoint(&actual)
				return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("提交 Bootstrap LKG 失败: %w", err)
			}
			r.BootstrapInventory = &inventory
		}
		if err := r.publishBootstrapReferences(ctx, &actual); err != nil {
			actual.Errors = append(actual.Errors, OperationError{Stage: "bootstrap_reference", Message: err.Error()})
			_ = r.checkpoint(&actual)
			return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("发布 Seed/LKG 制品引用失败: %w", err)
		}
		if collector, ok := r.Installer.(GarbageCollector); ok {
			if err := collector.GarbageCollect(referencedSHA256(actual)); err != nil {
				actual.Errors = append(actual.Errors, OperationError{Stage: "gc", Message: err.Error()})
				_ = r.checkpoint(&actual)
				return Result{Changed: changed, Converged: false, State: actual}, fmt.Errorf("安装目录垃圾回收失败: %w", err)
			}
		}
		// AppliedRevision is the transaction commit marker consumed by the
		// controller and startup gates. Runtime health alone is insufficient:
		// reference protection, Bootstrap LKG and installer GC must all succeed
		// before this revision can be reported as applied.
		actual.AppliedRevision = desired.Revision
		if err := r.checkpoint(&actual); err != nil {
			return Result{Changed: changed, Converged: false, State: actual}, err
		}
	}
	result := Result{Changed: changed, Converged: converged, State: actual}
	if !converged {
		failure := fmt.Errorf("节点 %s 未收敛：%d 个操作失败", r.NodeID, len(actual.Errors))
		for _, operation := range actual.Errors {
			failure = errors.Join(failure, fmt.Errorf("unit=%s stage=%s: %s", operation.UnitID, operation.Stage, operation.Message))
		}
		return result, failure
	}
	return result, nil
}

func (r *Reconciler) beginReconcile(desired deploymentv1.DesiredState) (ActualState, error) {
	if err := r.validate(); err != nil {
		return ActualState{}, err
	}
	actual, err := r.StateStore.Load()
	if err != nil {
		return ActualState{}, err
	}
	if r.BootstrapUpgrade != nil {
		inventory, err := r.BootstrapUpgrade.Begin(installedBootstrapItems(actual))
		if err != nil {
			return ActualState{}, fmt.Errorf("恢复 Bootstrap 升级事务: %w", err)
		}
		r.BootstrapInventory = &inventory
	}
	if actual.NodeID != "" && actual.NodeID != r.NodeID {
		return ActualState{}, fmt.Errorf("实际态属于节点 %q，当前节点为 %q", actual.NodeID, r.NodeID)
	}
	digest := desired.Digest()
	if actual.ObservedRevision == desired.Revision && actual.ObservedDigest != "" && actual.ObservedDigest != digest {
		return ActualState{}, fmt.Errorf("revision %d 的期望态内容发生冲突", desired.Revision)
	}
	actual.Version = actualStateVersion
	actual.NodeID = r.NodeID
	actual.ObservedRevision = desired.Revision
	actual.ObservedDigest = digest
	if desired.Metadata.Tenant != "" && desired.Metadata.Name != "" {
		ownerID := "assignment/" + desired.Metadata.Name + "/" + r.NodeID
		if actual.ReferenceOwnerID != "" && (actual.ReferenceTenant != desired.Metadata.Tenant || actual.ReferenceOwnerID != ownerID) {
			return ActualState{}, errors.New("实际态的 Assignment 引用 owner 与期望态不一致")
		}
		actual.ReferenceTenant, actual.ReferenceOwnerID = desired.Metadata.Tenant, ownerID
	}
	actual.Errors = nil
	return actual, nil
}

func (r *Reconciler) targetUnits(desired deploymentv1.DesiredState) map[string]deploymentv1.Unit {
	targets := make(map[string]deploymentv1.Unit)
	for _, unit := range desired.Units {
		if unit.Enabled && unit.MatchesNode(r.NodeLabels) {
			targets[unit.ID] = unit
		}
	}
	return targets
}
