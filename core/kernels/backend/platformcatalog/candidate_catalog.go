package platformcatalog

import (
	"errors"
	"fmt"
	"strings"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

func normalizePrepareRequest(request PrepareRequest) (PrepareRequest, error) {
	if !validPrefixedHex(request.CandidateID, "pcfg_", 32) || !validPrefixedHex(request.RequestDigest, "", 64) ||
		strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(request.DeploymentName) == "" ||
		!validPrefixedHex(request.ExpectedCatalogDigest, "", 64) || !validRef(request.ExpectedProfile) ||
		request.NextCatalogRevision == 0 || !validPrefixedHex(request.NextCatalogDigest, "", 64) {
		return PrepareRequest{}, errors.New("Backend Platform Profile 候选请求身份无效")
	}
	normalized, err := backendcompositionv1.ValidatePlatformProfile(request.NextProfile)
	if err != nil {
		return PrepareRequest{}, fmt.Errorf("Backend Platform Profile 候选修订无效: %w", err)
	}
	request.NextProfile = normalized
	return request, nil
}

func buildCandidateCatalog(active backendcompositionv1.BackendPlatformCatalog, request PrepareRequest) (backendcompositionv1.BackendPlatformCatalog, compositioncommonv1.Ref, error) {
	if active.Digest() != request.ExpectedCatalogDigest || request.NextCatalogRevision != active.Revision+1 {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, ErrCatalogConflict
	}
	_, previous, err := active.Resolve(request.TenantID, request.DeploymentName)
	if err != nil || previous != request.ExpectedProfile {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, ErrCatalogConflict
	}
	nextProfile, err := backendcompositionv1.ValidatePlatformProfile(request.NextProfile)
	if err != nil || nextProfile.ID != previous.ID || nextProfile.Revision <= previous.Revision {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, errors.New("候选 Profile 必须克隆同一 ID 并使用更高 revision")
	}
	for _, profile := range active.Profiles {
		if profile.ID == nextProfile.ID && profile.Revision >= nextProfile.Revision {
			return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, errors.New("候选 Profile revision 不单调")
		}
	}
	next := cloneCatalog(active)
	next.Revision = request.NextCatalogRevision
	next.Profiles = append(next.Profiles, nextProfile)
	nextRef := compositioncommonv1.Ref{ID: nextProfile.ID, Revision: nextProfile.Revision, Digest: nextProfile.Digest()}
	if !replaceBinding(&next, request.TenantID, request.DeploymentName, nextRef) {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, ErrCatalogConflict
	}
	next, err = backendcompositionv1.ValidateBackendPlatformCatalog(next)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, fmt.Errorf("构造 Backend Platform Catalog 候选: %w", err)
	}
	if next.Digest() != request.NextCatalogDigest {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, ErrCatalogConflict
	}
	return next, previous, nil
}

func replaceBinding(catalog *backendcompositionv1.BackendPlatformCatalog, tenantID, deploymentName string, profile compositioncommonv1.Ref) bool {
	for index := range catalog.Bindings {
		binding := &catalog.Bindings[index]
		if binding.TenantID == tenantID && binding.DeploymentName == deploymentName {
			binding.PlatformProfile = profile
			return true
		}
	}
	return false
}

func validateCandidateAgainstSnapshot(snapshot persistedSnapshot) error {
	if snapshot.Candidate == nil {
		return nil
	}
	candidate := *snapshot.Candidate
	if !validPrefixedHex(candidate.CandidateID, "pcfg_", 32) || !validPrefixedHex(candidate.RequestDigest, "", 64) ||
		!validPrefixedHex(candidate.ExpectedCatalogDigest, "", 64) || !validPrefixedHex(candidate.NextCatalogDigest, "", 64) ||
		!validRef(candidate.PreviousProfile) || strings.TrimSpace(candidate.TenantID) == "" || strings.TrimSpace(candidate.DeploymentName) == "" ||
		candidate.CreatedAt.IsZero() || candidate.UpdatedAt.Before(candidate.CreatedAt) {
		return errors.New("Backend Platform Profile 候选快照身份无效")
	}
	switch candidate.Status {
	case CandidatePrepared, CandidateAborted:
		if snapshot.Digest != candidate.ExpectedCatalogDigest {
			return errors.New("未激活候选与活动 Catalog 摘要不一致")
		}
		if _, _, err := buildCandidateCatalog(snapshot.Catalog, candidate.prepareRequest()); err != nil {
			return errors.New("未激活候选不能由活动 Catalog 确定性重建")
		}
	case CandidateActivated, CandidateFinalized:
		if snapshot.Digest != candidate.NextCatalogDigest || snapshot.Catalog.Revision != candidate.NextCatalogRevision ||
			!catalogHasProfile(snapshot.Catalog, candidate.PreviousProfile) || !catalogBindsProfile(snapshot.Catalog, candidate.TenantID, candidate.DeploymentName, candidate.NextProfile) {
			return errors.New("已激活候选与活动 Catalog 摘要不一致")
		}
	case CandidateRolledBack:
		if !validPrefixedHex(candidate.RollbackCatalogDigest, "", 64) || snapshot.Digest != candidate.RollbackCatalogDigest ||
			snapshot.Catalog.Revision <= candidate.NextCatalogRevision || !catalogBindsRef(snapshot.Catalog, candidate.TenantID, candidate.DeploymentName, candidate.PreviousProfile) {
			return errors.New("已回滚候选与活动 Catalog 摘要不一致")
		}
	default:
		return errors.New("Backend Platform Profile 候选状态无效")
	}
	return nil
}

func catalogHasProfile(catalog backendcompositionv1.BackendPlatformCatalog, ref compositioncommonv1.Ref) bool {
	for _, profile := range catalog.Profiles {
		if profile.ID == ref.ID && profile.Revision == ref.Revision && profile.Digest() == ref.Digest {
			return true
		}
	}
	return false
}

func catalogBindsProfile(catalog backendcompositionv1.BackendPlatformCatalog, tenantID, deploymentName string, profile backendcompositionv1.PlatformProfile) bool {
	ref := compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}
	return catalogHasProfile(catalog, ref) && catalogBindsRef(catalog, tenantID, deploymentName, ref)
}

func catalogBindsRef(catalog backendcompositionv1.BackendPlatformCatalog, tenantID, deploymentName string, ref compositioncommonv1.Ref) bool {
	for _, binding := range catalog.Bindings {
		if binding.TenantID == tenantID && binding.DeploymentName == deploymentName {
			return binding.PlatformProfile == ref
		}
	}
	return false
}
