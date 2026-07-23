// Package deploymentpublisher owns online Backend composition resolution and
// CAS publication. It is trusted kernel code, not a configurable plugin.
package deploymentpublisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nats-io/nats.go/jetstream"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/compositioncore"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type Applier interface {
	Apply(context.Context, string, []byte) (uint64, deploymentv2.Deployment, error)
}

type KVApplier struct{ KV jetstream.KeyValue }

// Resolver is injected by the Backend composition root. Keeping this narrow
// function boundary prevents the publisher from reaching sideways into the
// concrete composition-resolver package.
type Resolver func(backendcompositionv1.PlatformProfile, backendcompositionv1.ApplicationComposition, uint64, compositioncore.ArtifactReader, compositioncore.Options) (deploymentv2.Deployment, error)

// CatalogSource returns the current trusted Backend Platform Catalog snapshot.
// Static startup files and the future NATS-backed activation store implement
// the same narrow port; callers never receive a mutable catalog pointer.
type CatalogSource interface {
	Snapshot(context.Context) (backendcompositionv1.BackendPlatformCatalog, error)
}

type staticCatalogSource struct {
	catalog backendcompositionv1.BackendPlatformCatalog
}

func (s staticCatalogSource) Snapshot(context.Context) (backendcompositionv1.BackendPlatformCatalog, error) {
	return s.catalog, nil
}

func (a KVApplier) Apply(ctx context.Context, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	if a.KV == nil {
		return 0, deploymentv2.Deployment{}, errors.New("deployment KV 未配置")
	}
	return sharedcontrolplane.ApplyDeployment(ctx, a.KV, key, raw)
}

type Publisher struct {
	catalog   CatalogSource
	artifacts compositioncore.ArtifactReader
	options   compositioncore.Options
	applier   Applier
	catalogs  pluginconfiguration.Publisher
	resolve   Resolver
}

type recordingArtifactReader struct {
	delegate compositioncore.ArtifactReader
	values   map[pluginv1.ArtifactRef]pluginv1.Artifact
}

func (r *recordingArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, raw, err := r.delegate.Read(ref)
	if err == nil {
		r.values[ref] = artifact
	}
	return artifact, raw, err
}

func New(catalog backendcompositionv1.BackendPlatformCatalog, artifacts compositioncore.ArtifactReader, applier Applier, catalogs pluginconfiguration.Publisher, options compositioncore.Options, resolve Resolver) (*Publisher, error) {
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		return nil, err
	}
	return NewWithCatalogSource(staticCatalogSource{catalog: validated}, artifacts, applier, catalogs, options, resolve)
}

// NewWithCatalogSource enables an online, CAS-governed catalog without moving
// Profile ownership, NATS credentials or trust roots into deployment-manager.
func NewWithCatalogSource(catalog CatalogSource, artifacts compositioncore.ArtifactReader, applier Applier, catalogs pluginconfiguration.Publisher, options compositioncore.Options, resolve Resolver) (*Publisher, error) {
	if artifacts == nil || applier == nil || catalogs == nil || resolve == nil {
		return nil, errors.New("在线部署发布器必须配置可信制品读取器、Composition Resolver、Deployment CAS 与配置目录发布器")
	}
	if catalog == nil {
		return nil, errors.New("在线部署发布器必须配置可信 Backend Platform Catalog 源")
	}
	publisher := &Publisher{catalog: catalog, artifacts: artifacts, options: options, applier: applier, catalogs: catalogs, resolve: resolve}
	if _, err := publisher.catalogSnapshot(context.Background()); err != nil {
		return nil, err
	}
	return publisher, nil
}

func (p *Publisher) catalogSnapshot(ctx context.Context) (backendcompositionv1.BackendPlatformCatalog, error) {
	catalog, err := p.catalog.Snapshot(ctx)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, fmt.Errorf("读取 Backend Platform Catalog 快照: %w", err)
	}
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		return backendcompositionv1.BackendPlatformCatalog{}, fmt.Errorf("复核 Backend Platform Catalog 快照: %w", err)
	}
	return validated, nil
}

func (p *Publisher) Targets(ctx context.Context, tenantID string) ([]deploymentpublication.Target, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("部署目标 tenant 不能为空")
	}
	catalog, err := p.catalogSnapshot(ctx)
	if err != nil {
		return nil, err
	}
	bindings := catalog.Targets(tenantID)
	out := make([]deploymentpublication.Target, len(bindings))
	for i, binding := range bindings {
		out[i] = deploymentpublication.Target{DeploymentName: binding.DeploymentName, PlatformProfile: binding.PlatformProfile}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeploymentName < out[j].DeploymentName })
	return out, nil
}

func (p *Publisher) Preview(ctx context.Context, tenantID string, application backendcompositionv1.ApplicationComposition, deploymentRevision uint64) (deploymentpublication.Result, error) {
	if err := validateIdentity(tenantID, application); err != nil {
		return deploymentpublication.Result{}, err
	}
	catalog, err := p.catalogSnapshot(ctx)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	profile, _, err := catalog.Resolve(tenantID, application.Metadata.Name)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	recording := &recordingArtifactReader{delegate: p.artifacts, values: map[pluginv1.ArtifactRef]pluginv1.Artifact{}}
	resolved, err := p.resolve(profile, application, deploymentRevision, recording, p.options)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	references, err := resolvedArtifactReferences(resolved, recording.values)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	configurationCatalog, err := pluginconfiguration.Build(resolved, recording.values)
	if err != nil {
		return deploymentpublication.Result{}, fmt.Errorf("生成可信插件配置目录: %w", err)
	}
	return deploymentpublication.Result{Deployment: resolved, Digest: resolved.Digest(), ArtifactReferences: references, ConfigurationCatalog: configurationCatalog}, nil
}

func resolvedArtifactReferences(deployment deploymentv2.Deployment, artifacts map[pluginv1.ArtifactRef]pluginv1.Artifact) ([]pluginv1.ArtifactReference, error) {
	byRef := map[pluginv1.ArtifactRef]pluginv1.ArtifactReference{}
	for _, unit := range deployment.Units {
		for _, plugin := range unit.Plugins {
			ref := pluginv1.ArtifactRef{PluginID: plugin.ID, Version: plugin.Version, Channel: compositioncore.NormalizeChannel(plugin.Channel)}
			artifact, ok := artifacts[ref]
			if !ok || artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || compositioncore.NormalizeChannel(artifact.Channel) != ref.Channel || len(artifact.SHA256) != 64 {
				return nil, fmt.Errorf("可信部署预览缺少精确制品事实: %s@%s/%s", ref.PluginID, ref.Version, ref.Channel)
			}
			byRef[ref] = pluginv1.ArtifactReference{Ref: ref, SHA256: artifact.SHA256, Purpose: "resolved"}
		}
	}
	values := make([]pluginv1.ArtifactReference, 0, len(byRef))
	for _, value := range byRef {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Ref.PluginID != values[j].Ref.PluginID {
			return values[i].Ref.PluginID < values[j].Ref.PluginID
		}
		if values[i].Ref.Version != values[j].Ref.Version {
			return values[i].Ref.Version < values[j].Ref.Version
		}
		return values[i].Ref.Channel < values[j].Ref.Channel
	})
	return values, nil
}

func (p *Publisher) Publish(ctx context.Context, tenantID string, application backendcompositionv1.ApplicationComposition, deploymentRevision uint64, expectedDigest string) (deploymentpublication.Result, error) {
	preview, err := p.Preview(ctx, tenantID, application, deploymentRevision)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	if expectedDigest == "" || preview.Digest != expectedDigest {
		return deploymentpublication.Result{}, errors.New("发布预览摘要已变化，必须重新预览和审批")
	}
	raw, err := json.Marshal(preview.Deployment)
	if err != nil {
		return deploymentpublication.Result{}, fmt.Errorf("编码在线 Deployment v2: %w", err)
	}
	key := sharedcontrolplane.DeploymentKey(tenantID, application.Metadata.Name)
	kvRevision, applied, err := p.applier.Apply(ctx, key, raw)
	if err != nil {
		return deploymentpublication.Result{}, fmt.Errorf("CAS 发布在线 Deployment v2: %w", err)
	}
	preview.Deployment = applied
	preview.Digest = applied.Digest()
	preview.KVRevision = kvRevision
	if err := p.catalogs.Publish(ctx, tenantID, preview.ConfigurationCatalog); err != nil {
		return deploymentpublication.Result{}, fmt.Errorf("发布可信插件配置目录: %w", err)
	}
	return preview, nil
}

func validateIdentity(tenantID string, application backendcompositionv1.ApplicationComposition) error {
	if strings.TrimSpace(tenantID) == "" || application.Metadata.Tenant != tenantID || strings.TrimSpace(application.Metadata.Name) == "" || application.ID != application.Metadata.Name {
		return errors.New("Application Composition identity 必须与认证 tenant 和 deployment name 精确一致")
	}
	return nil
}
