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
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

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
