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
	"cdsoft.com.cn/VastPlan/core/shared/go/compositioncore"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/deploymentpublication"
)

type Applier interface {
	Apply(context.Context, string, []byte) (uint64, deploymentv2.Deployment, error)
}

type KVApplier struct{ KV jetstream.KeyValue }

// Resolver is injected by the Backend composition root. Keeping this narrow
// function boundary prevents the publisher from reaching sideways into the
// concrete composition-resolver package.
type Resolver func(backendcompositionv1.PlatformProfile, backendcompositionv1.ApplicationComposition, uint64, compositioncore.ArtifactReader, compositioncore.Options) (deploymentv2.Deployment, error)

func (a KVApplier) Apply(ctx context.Context, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	if a.KV == nil {
		return 0, deploymentv2.Deployment{}, errors.New("deployment KV 未配置")
	}
	return sharedcontrolplane.ApplyDeployment(ctx, a.KV, key, raw)
}

type Publisher struct {
	catalog   backendcompositionv1.BackendPlatformCatalog
	artifacts compositioncore.ArtifactReader
	options   compositioncore.Options
	applier   Applier
	resolve   Resolver
}

func New(catalog backendcompositionv1.BackendPlatformCatalog, artifacts compositioncore.ArtifactReader, applier Applier, options compositioncore.Options, resolve Resolver) (*Publisher, error) {
	validated, err := backendcompositionv1.ValidateBackendPlatformCatalog(catalog)
	if err != nil {
		return nil, err
	}
	if artifacts == nil || applier == nil || resolve == nil {
		return nil, errors.New("在线部署发布器必须配置可信制品读取器、Composition Resolver 与 CAS applier")
	}
	return &Publisher{catalog: validated, artifacts: artifacts, options: options, applier: applier, resolve: resolve}, nil
}

func (p *Publisher) Targets(_ context.Context, tenantID string) ([]deploymentpublication.Target, error) {
	if strings.TrimSpace(tenantID) == "" {
		return nil, errors.New("部署目标 tenant 不能为空")
	}
	bindings := p.catalog.Targets(tenantID)
	out := make([]deploymentpublication.Target, len(bindings))
	for i, binding := range bindings {
		out[i] = deploymentpublication.Target{DeploymentName: binding.DeploymentName, PlatformProfile: binding.PlatformProfile}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeploymentName < out[j].DeploymentName })
	return out, nil
}

func (p *Publisher) Preview(_ context.Context, tenantID string, application backendcompositionv1.ApplicationComposition, deploymentRevision uint64) (deploymentpublication.Result, error) {
	if err := validateIdentity(tenantID, application); err != nil {
		return deploymentpublication.Result{}, err
	}
	profile, _, err := p.catalog.Resolve(tenantID, application.Metadata.Name)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	resolved, err := p.resolve(profile, application, deploymentRevision, p.artifacts, p.options)
	if err != nil {
		return deploymentpublication.Result{}, err
	}
	return deploymentpublication.Result{Deployment: resolved, Digest: resolved.Digest()}, nil
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
	return preview, nil
}

func validateIdentity(tenantID string, application backendcompositionv1.ApplicationComposition) error {
	if strings.TrimSpace(tenantID) == "" || application.Metadata.Tenant != tenantID || strings.TrimSpace(application.Metadata.Name) == "" || application.ID != application.Metadata.Name {
		return errors.New("Application Composition identity 必须与认证 tenant 和 deployment name 精确一致")
	}
	return nil
}
