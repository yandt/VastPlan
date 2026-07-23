package profileactivation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/platformcatalog"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type memoryProfileStore struct {
	active    backendcompositionv1.BackendPlatformCatalog
	candidate *platformcatalog.Candidate
}

func (s *memoryProfileStore) Snapshot(context.Context) (backendcompositionv1.BackendPlatformCatalog, error) {
	return cloneJSON(s.active), nil
}

func (s *memoryProfileStore) Prepare(_ context.Context, request platformcatalog.PrepareRequest) (platformcatalog.Candidate, error) {
	if s.candidate != nil {
		if s.candidate.CandidateID == request.CandidateID && s.candidate.RequestDigest == request.RequestDigest {
			return *s.candidate, nil
		}
		return platformcatalog.Candidate{}, platformcatalog.ErrCandidateLocked
	}
	next, previous, err := buildCandidateForMemory(s.active, request)
	if err != nil {
		return platformcatalog.Candidate{}, err
	}
	candidate := platformcatalog.Candidate{
		CandidateID: request.CandidateID, RequestDigest: request.RequestDigest, ConfigurationID: request.ConfigurationID,
		TenantID: request.TenantID, DeploymentName: request.DeploymentName, ExpectedCatalogDigest: request.ExpectedCatalogDigest,
		PreviousProfile: previous, NextProfile: request.NextProfile, NextCatalogRevision: request.NextCatalogRevision,
		NextCatalogDigest: next.Digest(), Status: platformcatalog.CandidatePrepared,
	}
	s.candidate = &candidate
	return candidate, nil
}

func (s *memoryProfileStore) Candidate(_ context.Context, id, digest string) (platformcatalog.Candidate, error) {
	if s.candidate == nil || s.candidate.CandidateID != id || s.candidate.RequestDigest != digest {
		return platformcatalog.Candidate{}, platformcatalog.ErrCandidateNotFound
	}
	return *s.candidate, nil
}

func (s *memoryProfileStore) Activate(_ context.Context, id, digest string) (platformcatalog.Candidate, error) {
	return s.transition(id, digest, platformcatalog.CandidatePrepared, platformcatalog.CandidateActivated)
}
func (s *memoryProfileStore) Finalize(_ context.Context, id, digest string) (platformcatalog.Candidate, error) {
	return s.transition(id, digest, platformcatalog.CandidateActivated, platformcatalog.CandidateFinalized)
}
func (s *memoryProfileStore) Abort(_ context.Context, id, digest string) (platformcatalog.Candidate, error) {
	return s.transition(id, digest, platformcatalog.CandidatePrepared, platformcatalog.CandidateAborted)
}
func (s *memoryProfileStore) Rollback(_ context.Context, id, digest string) (platformcatalog.Candidate, error) {
	candidate, err := s.Candidate(context.Background(), id, digest)
	if err != nil || candidate.Status != platformcatalog.CandidateActivated {
		return platformcatalog.Candidate{}, platformcatalog.ErrInvalidTransition
	}
	candidate.Status, candidate.RollbackCatalogDigest = platformcatalog.CandidateRolledBack, strings.Repeat("f", 64)
	s.candidate = &candidate
	return candidate, nil
}

func (s *memoryProfileStore) transition(id, digest string, from, to platformcatalog.CandidateStatus) (platformcatalog.Candidate, error) {
	candidate, err := s.Candidate(context.Background(), id, digest)
	if err != nil {
		return platformcatalog.Candidate{}, err
	}
	if candidate.Status == to {
		return candidate, nil
	}
	if candidate.Status != from {
		return platformcatalog.Candidate{}, platformcatalog.ErrInvalidTransition
	}
	candidate.Status = to
	s.candidate = &candidate
	return candidate, nil
}

type catalogReader struct{ values []pluginconfiguration.Catalog }

func (r catalogReader) List(context.Context, string) ([]pluginconfiguration.Catalog, error) {
	return r.values, nil
}

type candidatePublisher struct {
	store     *memoryProfileStore
	artifact  pluginv1.Artifact
	published bool
}

func (p *candidatePublisher) PreviewCandidate(_ context.Context, tenant string, application backendcompositionv1.ApplicationComposition, revision uint64, id, digest string) (deploymentpublication.Result, error) {
	candidate, err := p.store.Candidate(context.Background(), id, digest)
	if err != nil || candidate.TenantID != tenant || candidate.DeploymentName != application.Metadata.Name {
		return deploymentpublication.Result{}, platformcatalog.ErrCandidateNotFound
	}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: revision, Metadata: application.Metadata, Units: cloneJSON(candidate.NextProfile.Services),
		Resolution: deploymentv2.Resolution{PlatformProfile: profileRef(candidate.NextProfile), ApplicationComposition: compositioncommonv1.Ref{ID: application.ID, Revision: application.Revision, Digest: application.Digest()}, PluginOrigins: map[string]string{p.artifact.PluginID: deploymentv2.OriginPlatformProfile}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{{PluginID: p.artifact.PluginID, Version: p.artifact.Version, Channel: p.artifact.Channel}: p.artifact})
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	return deploymentpublication.Result{Deployment: deployment, Digest: deployment.Digest(), PlatformCatalogDigest: candidate.NextCatalogDigest, PlatformProfile: profileRef(candidate.NextProfile), ConfigurationCatalog: catalog}, nil
}

func (p *candidatePublisher) PublishCandidate(ctx context.Context, tenant string, application backendcompositionv1.ApplicationComposition, revision uint64, expected, id, digest string) (deploymentpublication.Result, error) {
	if p.store.candidate == nil || p.store.candidate.Status != platformcatalog.CandidateActivated {
		return deploymentpublication.Result{}, platformcatalog.ErrInvalidTransition
	}
	preview, err := p.PreviewCandidate(ctx, tenant, application, revision, id, digest)
	if err != nil || preview.Digest != expected {
		return deploymentpublication.Result{}, errors.New("preview changed")
	}
	p.published = true
	return preview, nil
}

func TestControllerRunsPreparedActivatedPublishedFinalizedLifecycle(t *testing.T) {
	active := profileTestCatalog(t)
	artifact := profileTestArtifact()
	initialCatalog := configurationCatalogFor(t, active.Profiles[0], 4, artifact, "east")
	definition := initialCatalog.Items[0]
	store := &memoryProfileStore{active: active}
	publisher := &candidatePublisher{store: store, artifact: artifact}
	controller, err := New(store, catalogReader{values: []pluginconfiguration.Catalog{initialCatalog}}, publisher)
	if err != nil {
		t.Fatal(err)
	}
	request := platformprofileactivation.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("1", 32), ConfigurationID: definition.ID,
		ConfigCatalogDigest: initialCatalog.Digest, SchemaDigest: definition.SchemaDigest, ArtifactSHA256: definition.Artifact.SHA256,
		Values: json.RawMessage(`{"region":"west"}`), Composition: profileTestApplication(), DeploymentRevision: 5,
	}
	prepared, err := controller.Prepare(context.Background(), "tenant-a", request)
	if err != nil || prepared.Candidate.Status != platformprofileactivation.StatusPrepared || prepared.Preview.ConfigurationCatalog.Items[0].Values == nil {
		t.Fatalf("准备 Profile 候选失败: result=%+v err=%v", prepared, err)
	}
	digest := prepared.Candidate.RequestDigest
	if repeated, err := controller.Prepare(context.Background(), "tenant-a", request); err != nil || repeated.Candidate.RequestDigest != digest {
		t.Fatalf("Prepare 重试必须幂等: result=%+v err=%v", repeated, err)
	}
	key := platformprofileactivation.CandidateRequest{CandidateID: request.CandidateID, RequestDigest: digest}
	if _, err := controller.Publish(context.Background(), "tenant-a", platformprofileactivation.PublishRequest{Prepare: request, RequestDigest: digest, ExpectedDigest: prepared.Preview.Digest}); !errors.Is(err, platformcatalog.ErrInvalidTransition) {
		t.Fatalf("激活前不得发布候选: %v", err)
	}
	activated, err := controller.Activate(context.Background(), "tenant-a", key)
	if err != nil || activated.Status != platformprofileactivation.StatusActivated {
		t.Fatalf("激活候选失败: candidate=%+v err=%v", activated, err)
	}
	if _, err := controller.Publish(context.Background(), "tenant-a", platformprofileactivation.PublishRequest{Prepare: request, RequestDigest: digest, ExpectedDigest: prepared.Preview.Digest}); err != nil || !publisher.published {
		t.Fatalf("发布已激活候选失败: published=%v err=%v", publisher.published, err)
	}
	finalized, err := controller.Finalize(context.Background(), "tenant-a", key)
	if err != nil || finalized.Status != platformprofileactivation.StatusFinalized {
		t.Fatalf("完成候选失败: candidate=%+v err=%v", finalized, err)
	}
	if _, err := controller.Status(context.Background(), "other", key); !errors.Is(err, platformcatalog.ErrCandidateNotFound) {
		t.Fatalf("跨租户不得观察候选: %v", err)
	}
}

func profileTestArtifact() pluginv1.Artifact {
	pluginID := "cn.vastplan.platform.example.configurable"
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"Configurable","description":"test","version":"1.0.0","publisher":"vastplan","engines":{"backend":"^0.1"},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"properties":{"region":{"type":"string"}},"required":["region"]}},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, pluginID))
	return pluginv1.Artifact{PluginID: pluginID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("b", 64), Manifest: manifest}
}

func configurationCatalogFor(t *testing.T, profile backendcompositionv1.PlatformProfile, revision uint64, artifact pluginv1.Artifact, region string) pluginconfiguration.Catalog {
	t.Helper()
	services := cloneJSON(profile.Services)
	services[0].Config = map[string]any{"plugins": map[string]any{artifact.PluginID: map[string]any{"region": region}}}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: revision, Metadata: deploymentv1.Metadata{Name: "services-a", Tenant: "tenant-a"}, Units: services,
		Resolution: deploymentv2.Resolution{PlatformProfile: profileRef(profile), ApplicationComposition: compositioncommonv1.Ref{ID: "services-a", Revision: 1, Digest: strings.Repeat("a", 64)}, PluginOrigins: map[string]string{artifact.PluginID: deploymentv2.OriginPlatformProfile}},
	}
	catalog, err := pluginconfiguration.Build(deployment, map[pluginv1.ArtifactRef]pluginv1.Artifact{{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}: artifact})
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}

func profileTestApplication() backendcompositionv1.ApplicationComposition {
	return backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "services-a"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		Metadata: deploymentv1.Metadata{Name: "services-a", Tenant: "tenant-a"}, Units: []backendcompositionv1.ApplicationUnit{},
	}
}

func buildCandidateForMemory(active backendcompositionv1.BackendPlatformCatalog, request platformcatalog.PrepareRequest) (backendcompositionv1.BackendPlatformCatalog, compositioncommonv1.Ref, error) {
	_, previous, err := active.Resolve(request.TenantID, request.DeploymentName)
	if err != nil || previous != request.ExpectedProfile {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, platformcatalog.ErrCatalogConflict
	}
	next, err := buildCatalogCandidate(active, request.TenantID, request.DeploymentName, request.NextProfile)
	if err != nil || next.Digest() != request.NextCatalogDigest {
		return backendcompositionv1.BackendPlatformCatalog{}, compositioncommonv1.Ref{}, platformcatalog.ErrCatalogConflict
	}
	return next, previous, nil
}
