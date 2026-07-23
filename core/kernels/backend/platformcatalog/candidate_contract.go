package platformcatalog

import (
	"errors"
	"strings"
	"time"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
)

var (
	ErrCatalogNotSeeded  = errors.New("Backend Platform Catalog 尚未由 Bootstrap 持久化")
	ErrCatalogConflict   = errors.New("Backend Platform Catalog 前置摘要冲突")
	ErrCandidateLocked   = errors.New("Backend Platform Catalog 已有活动候选")
	ErrBindingLocked     = errors.New("Backend Platform binding 正在执行 Profile 激活")
	ErrCandidateNotFound = errors.New("Backend Platform Profile 候选不存在")
	ErrInvalidTransition = errors.New("Backend Platform Profile 候选状态迁移无效")
)

type CandidateStatus string

const (
	CandidatePrepared   CandidateStatus = "Prepared"
	CandidateActivated  CandidateStatus = "Activated"
	CandidateFinalized  CandidateStatus = "Finalized"
	CandidateAborted    CandidateStatus = "Aborted"
	CandidateRolledBack CandidateStatus = "RolledBack"
)

type PrepareRequest struct {
	CandidateID           string                               `json:"candidateId"`
	RequestDigest         string                               `json:"requestDigest"`
	ConfigurationID       string                               `json:"configurationId"`
	TenantID              string                               `json:"tenantId"`
	DeploymentName        string                               `json:"deploymentName"`
	ExpectedCatalogDigest string                               `json:"expectedCatalogDigest"`
	ExpectedProfile       compositioncommonv1.Ref              `json:"expectedProfile"`
	NextProfile           backendcompositionv1.PlatformProfile `json:"nextProfile"`
	NextCatalogRevision   uint64                               `json:"nextCatalogRevision"`
	NextCatalogDigest     string                               `json:"nextCatalogDigest"`
}

// Candidate is durable recovery state, not a browser DTO. It contains no
// credential material; Profile service config may contain only managed refs.
type Candidate struct {
	CandidateID           string                               `json:"candidateId"`
	RequestDigest         string                               `json:"requestDigest"`
	ConfigurationID       string                               `json:"configurationId"`
	TenantID              string                               `json:"tenantId"`
	DeploymentName        string                               `json:"deploymentName"`
	ExpectedCatalogDigest string                               `json:"expectedCatalogDigest"`
	PreviousProfile       compositioncommonv1.Ref              `json:"previousProfile"`
	NextProfile           backendcompositionv1.PlatformProfile `json:"nextProfile"`
	NextCatalogRevision   uint64                               `json:"nextCatalogRevision"`
	NextCatalogDigest     string                               `json:"nextCatalogDigest"`
	RollbackCatalogDigest string                               `json:"rollbackCatalogDigest,omitempty"`
	Status                CandidateStatus                      `json:"status"`
	CreatedAt             time.Time                            `json:"createdAt"`
	UpdatedAt             time.Time                            `json:"updatedAt"`
}

func (c Candidate) locks(tenantID, deploymentName string) bool {
	return c.TenantID == tenantID && c.DeploymentName == deploymentName &&
		(c.Status == CandidatePrepared || c.Status == CandidateActivated)
}

func requireCandidate(candidate *Candidate, candidateID, requestDigest string) (Candidate, error) {
	if candidate == nil || candidate.CandidateID != candidateID || candidate.RequestDigest != requestDigest {
		return Candidate{}, ErrCandidateNotFound
	}
	return *candidate, nil
}

func (c Candidate) prepareRequest() PrepareRequest {
	return PrepareRequest{
		CandidateID: c.CandidateID, RequestDigest: c.RequestDigest, ConfigurationID: c.ConfigurationID, TenantID: c.TenantID,
		DeploymentName: c.DeploymentName, ExpectedCatalogDigest: c.ExpectedCatalogDigest,
		ExpectedProfile: c.PreviousProfile, NextProfile: c.NextProfile,
		NextCatalogRevision: c.NextCatalogRevision, NextCatalogDigest: c.NextCatalogDigest,
	}
}

func validRef(ref compositioncommonv1.Ref) bool {
	return strings.TrimSpace(ref.ID) != "" && ref.Revision > 0 && validPrefixedHex(ref.Digest, "", 64)
}

func validPrefixedHex(value, prefix string, length int) bool {
	if !strings.HasPrefix(value, prefix) || len(value) != len(prefix)+length {
		return false
	}
	for _, character := range value[len(prefix):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
