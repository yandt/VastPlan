// Package profileactivation owns the trusted Platform Profile configuration
// transformation. Deployment Manager supplies an exact candidate; this package
// alone reads and mutates Platform Catalog state.
package profileactivation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/platformcatalog"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type CatalogStore interface {
	Snapshot(context.Context) (backendcompositionv1.BackendPlatformCatalog, error)
	Prepare(context.Context, platformcatalog.PrepareRequest) (platformcatalog.Candidate, error)
	Candidate(context.Context, string, string) (platformcatalog.Candidate, error)
	Activate(context.Context, string, string) (platformcatalog.Candidate, error)
	Finalize(context.Context, string, string) (platformcatalog.Candidate, error)
	Abort(context.Context, string, string) (platformcatalog.Candidate, error)
	Rollback(context.Context, string, string) (platformcatalog.Candidate, error)
}

type CandidatePublisher interface {
	PreviewCandidate(context.Context, string, backendcompositionv1.ApplicationComposition, uint64, string, string) (deploymentpublication.Result, error)
	PublishCandidate(context.Context, string, backendcompositionv1.ApplicationComposition, uint64, string, string, string) (deploymentpublication.Result, error)
}

type Controller struct {
	Catalogs       CatalogStore
	Configurations pluginconfiguration.Reader
	Deployments    CandidatePublisher
}

func New(catalogs CatalogStore, configurations pluginconfiguration.Reader, deployments CandidatePublisher) (*Controller, error) {
	if catalogs == nil || configurations == nil || deployments == nil {
		return nil, errors.New("Platform Profile Activation 必须配置 Catalog、配置目录和 Deployment 发布器")
	}
	return &Controller{Catalogs: catalogs, Configurations: configurations, Deployments: deployments}, nil
}

func (c *Controller) Prepare(ctx context.Context, tenantID string, request platformprofileactivation.PrepareRequest) (platformprofileactivation.PrepareResult, error) {
	normalized, err := platformprofileactivation.NormalizePrepareRequest(request)
	if err != nil || normalized.Composition.Metadata.Tenant != tenantID {
		return platformprofileactivation.PrepareResult{}, errors.New("Platform Profile 配置候选与认证租户不匹配")
	}
	requestDigest, err := platformprofileactivation.DigestPrepareRequest(normalized)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	if existing, err := c.Catalogs.Candidate(ctx, normalized.CandidateID, requestDigest); err == nil {
		if existing.ConfigurationID != normalized.ConfigurationID || existing.TenantID != tenantID || existing.DeploymentName != normalized.Composition.Metadata.Name {
			return platformprofileactivation.PrepareResult{}, platformcatalog.ErrCatalogConflict
		}
		return c.preparedResult(ctx, tenantID, normalized, existing)
	} else if !errors.Is(err, platformcatalog.ErrCandidateNotFound) {
		return platformprofileactivation.PrepareResult{}, err
	}
	definition, err := c.configurationDefinition(ctx, tenantID, normalized)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	active, err := c.Catalogs.Snapshot(ctx)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	nextProfile, previousProfile, err := buildProfileCandidate(active, tenantID, normalized.Composition.Metadata.Name, definition, normalized)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	nextCatalog, err := buildCatalogCandidate(active, tenantID, normalized.Composition.Metadata.Name, nextProfile)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	candidate, err := c.Catalogs.Prepare(ctx, platformcatalog.PrepareRequest{
		CandidateID: normalized.CandidateID, RequestDigest: requestDigest, ConfigurationID: normalized.ConfigurationID,
		TenantID: tenantID, DeploymentName: normalized.Composition.Metadata.Name,
		ExpectedCatalogDigest: active.Digest(), ExpectedProfile: previousProfile, NextProfile: nextProfile,
		NextCatalogRevision: nextCatalog.Revision, NextCatalogDigest: nextCatalog.Digest(),
	})
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	return c.preparedResult(ctx, tenantID, normalized, candidate)
}

func (c *Controller) preparedResult(ctx context.Context, tenantID string, request platformprofileactivation.PrepareRequest, candidate platformcatalog.Candidate) (platformprofileactivation.PrepareResult, error) {
	preview, err := c.Deployments.PreviewCandidate(ctx, tenantID, request.Composition, request.DeploymentRevision, candidate.CandidateID, candidate.RequestDigest)
	if err != nil {
		return platformprofileactivation.PrepareResult{}, err
	}
	view := candidateView(candidate)
	if err := view.Validate(); err != nil || preview.PlatformCatalogDigest != view.NextCatalogDigest || preview.PlatformProfile != view.NextProfile || !previewMatches(preview.ConfigurationCatalog, request) {
		return platformprofileactivation.PrepareResult{}, errors.New("Platform Profile 候选预览与配置请求不一致")
	}
	return platformprofileactivation.PrepareResult{Candidate: view, Preview: preview}, nil
}

func (c *Controller) Status(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error) {
	if err := request.Validate(); err != nil {
		return platformprofileactivation.Candidate{}, err
	}
	candidate, err := c.Catalogs.Candidate(ctx, request.CandidateID, request.RequestDigest)
	if err != nil {
		return platformprofileactivation.Candidate{}, err
	}
	if candidate.TenantID != tenantID {
		return platformprofileactivation.Candidate{}, platformcatalog.ErrCandidateNotFound
	}
	return candidateView(candidate), nil
}

func (c *Controller) Activate(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error) {
	return c.mutate(ctx, tenantID, request, c.Catalogs.Activate)
}

func (c *Controller) Finalize(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error) {
	return c.mutate(ctx, tenantID, request, c.Catalogs.Finalize)
}

func (c *Controller) Abort(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error) {
	return c.mutate(ctx, tenantID, request, c.Catalogs.Abort)
}

func (c *Controller) Rollback(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest) (platformprofileactivation.Candidate, error) {
	return c.mutate(ctx, tenantID, request, c.Catalogs.Rollback)
}

func (c *Controller) mutate(ctx context.Context, tenantID string, request platformprofileactivation.CandidateRequest, mutate func(context.Context, string, string) (platformcatalog.Candidate, error)) (platformprofileactivation.Candidate, error) {
	current, err := c.Status(ctx, tenantID, request)
	if err != nil {
		return platformprofileactivation.Candidate{}, err
	}
	candidate, err := mutate(ctx, current.CandidateID, current.RequestDigest)
	if err != nil {
		return platformprofileactivation.Candidate{}, err
	}
	view := candidateView(candidate)
	return view, view.Validate()
}

func (c *Controller) Publish(ctx context.Context, tenantID string, request platformprofileactivation.PublishRequest) (deploymentpublication.Result, error) {
	normalized, err := request.Normalize()
	if err != nil || normalized.Prepare.Composition.Metadata.Tenant != tenantID {
		return deploymentpublication.Result{}, errors.New("Platform Profile 候选发布请求无效")
	}
	candidate, err := c.Status(ctx, tenantID, platformprofileactivation.CandidateRequest{CandidateID: normalized.Prepare.CandidateID, RequestDigest: normalized.RequestDigest})
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	if candidate.Status != platformprofileactivation.StatusActivated || candidate.ConfigurationID != normalized.Prepare.ConfigurationID || candidate.Deployment != normalized.Prepare.Composition.Metadata.Name {
		return deploymentpublication.Result{}, platformcatalog.ErrInvalidTransition
	}
	result, err := c.Deployments.PublishCandidate(ctx, tenantID, normalized.Prepare.Composition, normalized.Prepare.DeploymentRevision, normalized.ExpectedDigest, normalized.Prepare.CandidateID, normalized.RequestDigest)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	if result.PlatformCatalogDigest != candidate.NextCatalogDigest || result.PlatformProfile != candidate.NextProfile || !previewMatches(result.ConfigurationCatalog, normalized.Prepare) {
		return deploymentpublication.Result{}, errors.New("已发布 Platform Profile 候选与批准预览不一致")
	}
	return result, nil
}

func (c *Controller) configurationDefinition(ctx context.Context, tenantID string, request platformprofileactivation.PrepareRequest) (pluginconfiguration.Definition, error) {
	catalogs, err := c.Configurations.List(ctx, tenantID)
	if err != nil {
		return pluginconfiguration.Definition{}, err
	}
	var matched *pluginconfiguration.Definition
	for _, catalog := range catalogs {
		if catalog.Deployment != request.Composition.Metadata.Name || catalog.Digest != request.ConfigCatalogDigest {
			continue
		}
		for index := range catalog.Items {
			definition := catalog.Items[index]
			if definition.ID != request.ConfigurationID {
				continue
			}
			if matched != nil {
				return pluginconfiguration.Definition{}, errors.New("可信配置目录包含重复 Platform Profile 定义")
			}
			copy := definition
			matched = &copy
		}
	}
	if matched == nil {
		return pluginconfiguration.Definition{}, errors.New("活动可信配置目录不存在指定 Platform Profile 配置")
	}
	if matched.ApplyPath != pluginconfiguration.ApplyPlatformProfile || matched.Origin != "platform-profile" ||
		matched.SchemaDigest != request.SchemaDigest || matched.Artifact.SHA256 != request.ArtifactSHA256 || matched.Deployment != request.Composition.Metadata.Name {
		return pluginconfiguration.Definition{}, errors.New("配置定义不属于可编辑 Platform Profile service")
	}
	if err := pluginconfiguration.ValidateValues(*matched, request.Values); err != nil {
		return pluginconfiguration.Definition{}, fmt.Errorf("Platform Profile 配置值无效: %w", err)
	}
	return *matched, nil
}

func candidateView(candidate platformcatalog.Candidate) platformprofileactivation.Candidate {
	return platformprofileactivation.Candidate{
		CandidateID: candidate.CandidateID, RequestDigest: candidate.RequestDigest, ConfigurationID: candidate.ConfigurationID,
		Deployment: candidate.DeploymentName, PreviousProfile: candidate.PreviousProfile,
		NextProfile: profileRef(candidate.NextProfile), ExpectedCatalogDigest: candidate.ExpectedCatalogDigest,
		NextCatalogDigest: candidate.NextCatalogDigest, RollbackCatalogDigest: candidate.RollbackCatalogDigest,
		Status: platformprofileactivation.Status(candidate.Status),
	}
}

func previewMatches(catalog pluginconfiguration.Catalog, request platformprofileactivation.PrepareRequest) bool {
	if catalog.Validate() != nil || catalog.Deployment != request.Composition.Metadata.Name || catalog.DeploymentRevision != request.DeploymentRevision {
		return false
	}
	for _, definition := range catalog.Items {
		if definition.ID == request.ConfigurationID {
			return definition.ApplyPath == pluginconfiguration.ApplyPlatformProfile && definition.SchemaDigest == request.SchemaDigest &&
				definition.Artifact.SHA256 == request.ArtifactSHA256 && jsonEqual(definition.Values, request.Values)
		}
	}
	return false
}

func normalizeChannel(value string) string {
	if strings.TrimSpace(value) == "" {
		return "stable"
	}
	return value
}
