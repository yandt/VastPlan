package deploymentmanager

import (
	"context"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) RollbackTestRelease(ctx context.Context, host sdk.Host, call *contractv1.CallContext, id uint64) (platformadminapi.TestRelease, error) {
	tenant, err := callTenant(call)
	if err != nil || id == 0 {
		return platformadminapi.TestRelease{}, errInvalid
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	var release platformadminapi.TestRelease
	found := false
	for _, item := range state.TestReleases {
		if item.ID == id {
			release, found = item, true
			break
		}
	}
	if !found || release.Status != platformadminapi.TestReleaseFailed || !release.RollbackRequired || release.PreviousServiceRevisionID == 0 {
		s.mu.Unlock()
		return platformadminapi.TestRelease{}, errServiceState
	}
	active, activeErr := activeServiceRevision(state, state.TestBindings[release.BindingID].Deployment)
	s.mu.Unlock()
	if activeErr != nil {
		return platformadminapi.TestRelease{}, errServiceState
	}
	if active.ID == release.PreviousServiceRevisionID {
		_ = s.transitionTestRelease(tenant, id, platformadminapi.TestReleaseRolledBack, func(item *platformadminapi.TestRelease) {
			item.RollbackRequired = false
		})
		return s.testRelease(call, id)
	}
	if active.ID != release.CandidateServiceRevisionID {
		return platformadminapi.TestRelease{}, errServiceState
	}
	if err := s.transitionTestRelease(tenant, id, platformadminapi.TestReleaseRollingBack, nil); err != nil {
		return platformadminapi.TestRelease{}, err
	}
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
	defer cancel()
	rollback, err := s.RollbackServiceRevision(rollbackCtx, host, call, release.PreviousServiceRevisionID)
	if err == nil {
		_ = s.transitionTestRelease(tenant, id, platformadminapi.TestReleaseRollingBack, func(item *platformadminapi.TestRelease) {
			item.RollbackServiceRevisionID = rollback.ID
		})
		err = s.waitForServiceReadiness(rollbackCtx, host, call, rollback.Deployment, rollback.ID)
	}
	if err != nil {
		s.failTestRelease(tenant, id, "platform.test_release.rollback_failed", err, true)
		return s.testRelease(call, id)
	}
	_ = s.transitionTestRelease(tenant, id, platformadminapi.TestReleaseRolledBack, func(item *platformadminapi.TestRelease) {
		item.RollbackRequired = false
	})
	return s.testRelease(call, id)
}

func (s *Service) executeTestRelease(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, binding platformadminapi.TestTargetBinding, releaseID uint64) {
	if err := s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseResolving, nil); err != nil {
		return
	}
	release, err := s.testRelease(call, releaseID)
	if err != nil {
		return
	}
	entry, err := resolveTestArtifact(ctx, host, call, release)
	if err != nil || !contains(binding.AllowedPublishers, entry.Publisher) {
		s.failTestRelease(tenant, releaseID, "platform.test_release.artifact_rejected", coalesceError(err, errTestArtifact), false)
		return
	}
	if err := s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleasePreparing, nil); err != nil {
		return
	}
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	currentBinding, exists := state.TestBindings[binding.ID]
	active, activeErr := activeServiceRevision(state, binding.Deployment)
	if !exists || currentBinding.Version != binding.Version || !currentBinding.Enabled || activeErr != nil || validateBindingAgainstComposition(binding, active.Composition) != nil {
		s.mu.Unlock()
		s.failTestRelease(tenant, releaseID, "platform.test_release.target_changed", errTestArtifact, false)
		return
	}
	composition := cloneJSON(active.Composition)
	previousID := active.ID
	s.mu.Unlock()
	if !replaceBoundPlugin(&composition, binding, release.Receipt.Ref) {
		s.failTestRelease(tenant, releaseID, "platform.test_release.target_changed", errTestArtifact, false)
		return
	}
	if err := s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseValidating, func(item *platformadminapi.TestRelease) {
		item.PreviousServiceRevisionID = previousID
	}); err != nil {
		return
	}
	draft, err := s.CreateServiceDraft(ctx, host, call, composition)
	if err != nil {
		s.failTestRelease(tenant, releaseID, "platform.test_release.preview_failed", err, false)
		return
	}
	if err := s.authorizeTestReleaseRevision(tenant, draft.ID, releaseID, binding.ID); err != nil {
		s.failTestRelease(tenant, releaseID, "platform.test_release.authorization_failed", err, false)
		return
	}
	if err := s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseActivating, func(item *platformadminapi.TestRelease) {
		item.CandidateServiceRevisionID = draft.ID
	}); err != nil {
		return
	}
	candidate, err := s.PublishServiceRevision(ctx, host, call, draft.ID)
	if err == nil {
		err = s.waitForServiceReadiness(ctx, host, call, candidate.Deployment, candidate.ID)
	}
	if err == nil {
		_ = s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseReady, nil)
		return
	}
	s.rollbackFailedCandidate(ctx, host, call, tenant, releaseID, previousID, err)
}

func (s *Service) rollbackFailedCandidate(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string, releaseID, previousID uint64, cause error) {
	rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
	defer cancel()
	if err := s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseRollingBack, func(item *platformadminapi.TestRelease) {
		item.ErrorCode, item.ErrorMessage = "platform.test_release.candidate_not_ready", cause.Error()
		item.RollbackRequired = true
	}); err != nil {
		return
	}
	rollback, err := s.RollbackServiceRevision(rollbackCtx, host, call, previousID)
	if err == nil {
		_ = s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseRollingBack, func(item *platformadminapi.TestRelease) {
			item.RollbackServiceRevisionID = rollback.ID
		})
		err = s.waitForServiceReadiness(rollbackCtx, host, call, rollback.Deployment, rollback.ID)
	}
	if err != nil {
		s.failTestRelease(tenant, releaseID, "platform.test_release.rollback_failed", err, true)
		return
	}
	_ = s.transitionTestRelease(tenant, releaseID, platformadminapi.TestReleaseRolledBack, func(item *platformadminapi.TestRelease) {
		item.RollbackRequired = false
	})
}

func (s *Service) waitForServiceReadiness(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, revision uint64) error {
	interval := s.releasePollInterval
	if interval <= 0 {
		interval = 500 * time.Millisecond
	}
	var lastErr error
	for {
		var observation deploymentpublication.ReadinessObservation
		err := callKernelDeployment(ctx, host, call, deploymentpublication.KernelReadinessService, deploymentpublication.ReadinessRequest{DeploymentName: deployment, DeploymentRevision: revision}, &observation)
		if err == nil {
			if validateErr := observation.Validate(); validateErr != nil || observation.Deployment != deployment || observation.Revision != revision {
				lastErr = coalesceError(validateErr, errors.New("readiness observation 身份不匹配"))
			} else {
				switch observation.Status {
				case deploymentpublication.ReadinessReady:
					return nil
				case deploymentpublication.ReadinessFailed, deploymentpublication.ReadinessStopped:
					return fmt.Errorf("候选状态 %s: %s", observation.Status, observation.Reason)
				default:
					lastErr = fmt.Errorf("候选尚未就绪: %s", observation.Status)
				}
			}
		} else {
			lastErr = err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("等待候选就绪超时: %w", coalesceError(lastErr, ctx.Err()))
		case <-timer.C:
		}
	}
}
