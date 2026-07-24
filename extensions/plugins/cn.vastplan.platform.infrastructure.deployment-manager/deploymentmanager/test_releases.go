package deploymentmanager

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	semver "github.com/Masterminds/semver/v3"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var (
	errTestBindingConflict = errors.New("测试目标绑定版本冲突")
	errTestReleaseConflict = errors.New("测试目标已有进行中的发布")
	errTestArtifact        = errors.New("测试制品与目标绑定不匹配")
)

type artifactCatalogEntry struct {
	Ref                pluginv1.ArtifactRef `json:"ref"`
	SHA256             string               `json:"sha256"`
	Publisher          string               `json:"publisher"`
	RepositoryRevision uint64               `json:"repositoryRevision"`
	Targets            []string             `json:"targets"`
}

func (s *Service) ListTestTargetBindings(call *contractv1.CallContext) ([]platformadminapi.TestTargetBinding, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]platformadminapi.TestTargetBinding, 0, len(s.tenantLocked(tenant).TestBindings))
	for _, binding := range s.tenantLocked(tenant).TestBindings {
		out = append(out, cloneJSON(binding))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Service) PutTestTargetBinding(call *contractv1.CallContext, id string, request platformadminapi.PutTestTargetBindingRequest) (platformadminapi.TestTargetBinding, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.TestTargetBinding{}, err
	}
	id = strings.TrimSpace(id)
	publishers, err := normalizedPublishers(request.AllowedPublishers)
	if err != nil {
		return platformadminapi.TestTargetBinding{}, errInvalid
	}
	now := s.now().Format(time.RFC3339Nano)
	binding := platformadminapi.TestTargetBinding{
		ID: id, Kind: request.Kind, Deployment: strings.TrimSpace(request.Deployment),
		UnitID: strings.TrimSpace(request.UnitID), PluginID: strings.TrimSpace(request.PluginID),
		AllowedPublishers: publishers, Enabled: request.Enabled, UpdatedAt: now,
	}
	if err := validateTestBindingShape(binding); err != nil {
		return platformadminapi.TestTargetBinding{}, errInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	existing, exists := state.TestBindings[id]
	for otherID, other := range state.TestBindings {
		if otherID != id && sameTestTarget(other, binding) {
			return platformadminapi.TestTargetBinding{}, errTestBindingConflict
		}
	}
	for _, release := range state.TestReleases {
		if release.BindingID == id && !testReleaseTerminal(release.Status) {
			return platformadminapi.TestTargetBinding{}, errTestReleaseConflict
		}
	}
	if exists {
		if request.IfVersion == nil || *request.IfVersion != existing.Version {
			return platformadminapi.TestTargetBinding{}, errTestBindingConflict
		}
		binding.Version, binding.CreatedAt = existing.Version+1, existing.CreatedAt
	} else {
		if request.IfVersion != nil && *request.IfVersion != 0 {
			return platformadminapi.TestTargetBinding{}, errTestBindingConflict
		}
		binding.Version, binding.CreatedAt = 1, now
	}
	active, err := activeServiceRevision(state, binding.Deployment)
	if err != nil || validateBindingAgainstComposition(binding, active.Composition) != nil {
		return platformadminapi.TestTargetBinding{}, errInvalid
	}
	state.TestBindings[id] = binding
	if err := s.saveLocked(); err != nil {
		if exists {
			state.TestBindings[id] = existing
		} else {
			delete(state.TestBindings, id)
		}
		return platformadminapi.TestTargetBinding{}, err
	}
	return cloneJSON(binding), nil
}

func (s *Service) ListTestReleases(call *contractv1.CallContext) ([]platformadminapi.TestRelease, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]platformadminapi.TestRelease(nil), s.tenantLocked(tenant).TestReleases...)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// CreateTestRelease executes one serialized Backend candidate release. The
// operation is synchronous so its host callback authorization and call path
// remain tied to the authenticated request; every transition is durable.
func (s *Service) CreateTestRelease(ctx context.Context, host sdk.Host, call *contractv1.CallContext, request platformadminapi.CreateTestReleaseRequest) (platformadminapi.TestRelease, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.TestRelease{}, err
	}
	requester, err := actor(call)
	if err != nil || validateTestArtifactRequest(request) != nil {
		return platformadminapi.TestRelease{}, errInvalid
	}
	now := s.now().Format(time.RFC3339Nano)
	s.mu.Lock()
	state := s.tenantLocked(tenant)
	binding, exists := state.TestBindings[request.BindingID]
	if !exists || !binding.Enabled || binding.PluginID != request.Receipt.Ref.PluginID {
		s.mu.Unlock()
		return platformadminapi.TestRelease{}, errTestArtifact
	}
	for _, current := range state.TestReleases {
		if current.BindingID == binding.ID && !testReleaseTerminal(current.Status) {
			s.mu.Unlock()
			return platformadminapi.TestRelease{}, errTestReleaseConflict
		}
	}
	state.NextTestRelease++
	release := platformadminapi.TestRelease{
		ID: state.NextTestRelease, BindingID: binding.ID, Receipt: request.Receipt,
		Status: platformadminapi.TestReleaseQueued, RequestedBy: requester, CreatedAt: now, UpdatedAt: now,
	}
	state.TestReleases = append(state.TestReleases, release)
	if err := s.saveLocked(); err != nil {
		state.TestReleases = state.TestReleases[:len(state.TestReleases)-1]
		state.NextTestRelease--
		s.mu.Unlock()
		return platformadminapi.TestRelease{}, err
	}
	s.mu.Unlock()
	if err := publishTestReleaseReference(ctx, host, call, release); err != nil {
		_ = s.transitionTestRelease(tenant, release.ID, platformadminapi.TestReleaseFailed, func(current *platformadminapi.TestRelease) {
			current.ErrorCode = "platform.deployment.reference_protection_failed"
			current.ErrorMessage = "制品引用保护尚未提交，候选未激活"
		})
		return s.testRelease(call, release.ID)
	}

	releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
	defer cancel()
	s.executeTestRelease(releaseCtx, host, call, tenant, binding, release.ID)
	cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cleanupCancel()
	_ = releaseTestReleaseReference(cleanupCtx, host, call, release)
	return s.testRelease(call, release.ID)
}

func publishTestReleaseReference(ctx context.Context, host sdk.Host, call *contractv1.CallContext, release platformadminapi.TestRelease) error {
	return publishTestReleaseReferenceSnapshot(ctx, host, call, release, 1, []pluginv1.ArtifactReference{{Ref: release.Receipt.Ref, SHA256: release.Receipt.SHA256, Purpose: "test-release"}})
}

func releaseTestReleaseReference(ctx context.Context, host sdk.Host, call *contractv1.CallContext, release platformadminapi.TestRelease) error {
	return publishTestReleaseReferenceSnapshot(ctx, host, call, release, 2, []pluginv1.ArtifactReference{})
}

func publishTestReleaseReferenceSnapshot(ctx context.Context, host sdk.Host, call *contractv1.CallContext, release platformadminapi.TestRelease, generation uint64, references []pluginv1.ArtifactReference) error {
	if host == nil || call == nil {
		return errors.New("引用保护缺少可信宿主")
	}
	snapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerArtifactLock, OwnerID: fmt.Sprintf("deployment/test-release-%d", release.ID), Generation: generation,
		References: references,
	})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	operation := "putReferences"
	logicalService, routingDomain := platformadminapi.ArtifactsCapability, "platform"
	result, _, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("提交测试制品引用保护失败: %w", coalesceError(err, errTestArtifact))
	}
	return nil
}

// RollbackTestRelease recovers a fail-closed interrupted release. It never
// rewinds control-plane KV: when the candidate is active it republishes the
// previous composition as another monotonic service revision.
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
	if err != nil || !publisherAllowed(binding.AllowedPublishers, entry.Publisher) {
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

func resolveTestArtifact(ctx context.Context, host sdk.Host, call *contractv1.CallContext, release platformadminapi.TestRelease) (artifactCatalogEntry, error) {
	if host == nil {
		return artifactCatalogEntry{}, errors.New("测试发布缺少可信宿主")
	}
	request := struct {
		Receipt artifactrepositoryv1.Receipt `json:"receipt"`
		Target  string                       `json:"target"`
	}{Receipt: release.Receipt, Target: "backend"}
	raw, err := json.Marshal(request)
	if err != nil {
		return artifactCatalogEntry{}, err
	}
	operation := "listCatalog"
	logicalService, routingDomain := platformadminapi.ArtifactsCapability, "platform"
	result, payload, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return artifactCatalogEntry{}, fmt.Errorf("读取已验证制品目录失败: %w", coalesceError(err, errTestArtifact))
	}
	var entry artifactCatalogEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return artifactCatalogEntry{}, errTestArtifact
	}
	if entry.Ref != release.Receipt.Ref || !strings.EqualFold(entry.SHA256, release.Receipt.SHA256) || entry.RepositoryRevision != release.Receipt.Revision || !contains(entry.Targets, "backend") {
		return artifactCatalogEntry{}, errTestArtifact
	}
	return entry, nil
}

func (s *Service) authorizeTestReleaseRevision(tenant string, revisionID, releaseID uint64, bindingID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	index, err := serviceRevisionIndex(state, revisionID)
	if err != nil || state.Revisions[index].Status != platformadminapi.ServiceDraft {
		return errServiceState
	}
	old := state.Revisions[index]
	revision := old
	revision.Status = platformadminapi.ServiceApproved
	revision.SubmittedBy = fmt.Sprintf("test-release:%d", releaseID)
	revision.ApprovedBy = "test-target-binding:" + bindingID
	revision.UpdatedAt = s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	s.auditServiceLocked(state, revision, "service.revision.test_target_authorized", revision.ApprovedBy)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		return err
	}
	return nil
}

func (s *Service) transitionTestRelease(tenant string, id uint64, status platformadminapi.TestReleaseStatus, change func(*platformadminapi.TestRelease)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	for i := range state.TestReleases {
		if state.TestReleases[i].ID != id {
			continue
		}
		old := state.TestReleases[i]
		state.TestReleases[i].Status = status
		state.TestReleases[i].UpdatedAt = s.now().Format(time.RFC3339Nano)
		if change != nil {
			change(&state.TestReleases[i])
		}
		if err := s.saveLocked(); err != nil {
			state.TestReleases[i] = old
			return err
		}
		return nil
	}
	return errNotFound
}

func (s *Service) failTestRelease(tenant string, id uint64, code string, cause error, rollbackRequired bool) {
	_ = s.transitionTestRelease(tenant, id, platformadminapi.TestReleaseFailed, func(item *platformadminapi.TestRelease) {
		item.ErrorCode, item.ErrorMessage, item.RollbackRequired = code, cause.Error(), rollbackRequired
	})
}

func (s *Service) testRelease(call *contractv1.CallContext, id uint64) (platformadminapi.TestRelease, error) {
	tenant, err := callTenant(call)
	if err != nil {
		return platformadminapi.TestRelease{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, release := range s.tenantLocked(tenant).TestReleases {
		if release.ID == id {
			return release, nil
		}
	}
	return platformadminapi.TestRelease{}, errNotFound
}

func activeServiceRevision(state *tenantState, deployment string) (platformadminapi.ServiceRevision, error) {
	for _, revision := range state.Revisions {
		if revision.Deployment == deployment && revision.Status == platformadminapi.ServicePublished && revision.Active {
			return revision, nil
		}
	}
	return platformadminapi.ServiceRevision{}, errNotFound
}

func validateBindingAgainstComposition(binding platformadminapi.TestTargetBinding, composition backendcompositionv1.ApplicationComposition) error {
	if strings.HasPrefix(binding.PluginID, "cn.vastplan.foundation.") || strings.HasPrefix(binding.PluginID, "cn.vastplan.platform.") {
		return errors.New("测试目标绑定不得覆盖 foundation/platform 插件")
	}
	for _, unit := range composition.Units {
		if unit.Spec.ID != binding.UnitID {
			continue
		}
		for _, plugin := range unit.Spec.Plugins {
			if plugin.ID == binding.PluginID {
				return nil
			}
		}
	}
	return errors.New("测试目标绑定未命中现有应用插件槽位")
}

func replaceBoundPlugin(composition *backendcompositionv1.ApplicationComposition, binding platformadminapi.TestTargetBinding, artifact pluginv1.ArtifactRef) bool {
	for i := range composition.Units {
		if composition.Units[i].Spec.ID != binding.UnitID {
			continue
		}
		for j := range composition.Units[i].Spec.Plugins {
			if composition.Units[i].Spec.Plugins[j].ID == binding.PluginID {
				composition.Units[i].Spec.Plugins[j].Version = artifact.Version
				composition.Units[i].Spec.Plugins[j].Channel = artifact.Channel
				return true
			}
		}
	}
	return false
}

func validateTestBindingShape(binding platformadminapi.TestTargetBinding) error {
	if binding.ID == "" || binding.Kind != platformadminapi.TestTargetBackend || binding.Deployment == "" || binding.UnitID == "" || binding.PluginID == "" || len(binding.AllowedPublishers) == 0 {
		return errInvalid
	}
	return nil
}

func validateTestArtifactRequest(request platformadminapi.CreateTestReleaseRequest) error {
	if strings.TrimSpace(request.BindingID) == "" || request.Receipt.Ref.PluginID == "" || request.Receipt.Ref.Channel != "testing" && request.Receipt.Ref.Channel != "workspace" {
		return errInvalid
	}
	if err := artifactrepositoryv1.ValidateReceiptShape(request.Receipt); err != nil {
		return errInvalid
	}
	version, err := semver.StrictNewVersion(request.Receipt.Ref.Version)
	if err != nil || version.Prerelease() == "" {
		return errInvalid
	}
	digest, err := hex.DecodeString(request.Receipt.SHA256)
	if err != nil || len(digest) != 32 || request.Receipt.SHA256 != strings.ToLower(request.Receipt.SHA256) {
		return errInvalid
	}
	return nil
}

func normalizedPublishers(values []string) ([]string, error) {
	set := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, errInvalid
		}
		set[value] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for value := range set {
		out = append(out, value)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, errInvalid
	}
	return out, nil
}

func publisherAllowed(allowed []string, publisher string) bool { return contains(allowed, publisher) }

func sameTestTarget(left, right platformadminapi.TestTargetBinding) bool {
	return left.Kind == right.Kind && left.Deployment == right.Deployment && left.UnitID == right.UnitID && left.PluginID == right.PluginID
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func validTestReleaseStatus(status platformadminapi.TestReleaseStatus) bool {
	switch status {
	case platformadminapi.TestReleaseQueued, platformadminapi.TestReleaseResolving, platformadminapi.TestReleasePreparing,
		platformadminapi.TestReleaseValidating, platformadminapi.TestReleaseActivating, platformadminapi.TestReleaseReady,
		platformadminapi.TestReleaseRollingBack, platformadminapi.TestReleaseRolledBack, platformadminapi.TestReleaseFailed,
		platformadminapi.TestReleaseSuperseded:
		return true
	default:
		return false
	}
}

func testReleaseTerminal(status platformadminapi.TestReleaseStatus) bool {
	return status == platformadminapi.TestReleaseReady || status == platformadminapi.TestReleaseRolledBack || status == platformadminapi.TestReleaseFailed || status == platformadminapi.TestReleaseSuperseded
}

func coalesceError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}

func cloneJSON[T any](input T) T {
	raw, _ := json.Marshal(input)
	var output T
	_ = json.Unmarshal(raw, &output)
	return output
}
