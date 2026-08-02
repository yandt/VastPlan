package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactreference"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
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
		AllowInstall: request.AllowInstall, PortalTargets: normalizePortalTargets(request.PortalTargets),
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
