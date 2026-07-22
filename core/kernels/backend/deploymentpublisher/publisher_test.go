package deploymentpublisher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/compositionresolver"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

type artifactReader map[string]pluginv1.Artifact

func (r artifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, ok := r[ref.PluginID+"@"+ref.Version+"/"+ref.Channel]
	if !ok {
		return pluginv1.Artifact{}, nil, errors.New("not found")
	}
	return artifact, nil, nil
}

type memoryApplier struct {
	key string
	raw []byte
}

type memoryCatalogPublisher struct {
	tenant  string
	catalog pluginconfiguration.Catalog
}

func (p *memoryCatalogPublisher) Publish(_ context.Context, tenant string, catalog pluginconfiguration.Catalog) error {
	p.tenant, p.catalog = tenant, catalog
	return nil
}

func (a *memoryApplier) Apply(_ context.Context, key string, raw []byte) (uint64, deploymentv2.Deployment, error) {
	a.key, a.raw = key, append([]byte(nil), raw...)
	parsed, err := deploymentv2.Parse(raw)
	return 7, parsed, err
}

func TestPublisherUsesCatalogProfileAndDigestLock(t *testing.T) {
	applicationID := "com.example.agent"
	profile := backendcompositionv1.PlatformProfile{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "backend-default"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend}, ServiceClasses: []string{"application.backend"},
		Attachments: []backendcompositionv1.Attachment{}, Services: []deploymentv2.ServiceUnit{},
	}
	application := backendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "agent-services"}, Target: compositioncommonv1.Target{Kernel: compositioncommonv1.KernelBackend},
		Metadata: deploymentv1.Metadata{Name: "agent-services", Tenant: "acme"},
		Units: []backendcompositionv1.ApplicationUnit{{ServiceClass: "application.backend", Spec: deploymentv2.ServiceUnit{
			ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 2,
			Plugins: []deploymentv1.PluginRef{{ID: applicationID, Version: "1.0.0", Channel: "stable"}},
			Config:  map[string]any{"plugins": map[string]any{applicationID: map[string]any{"region": "cn-east"}}},
		}}},
	}
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "backend-production"}, Profiles: []backendcompositionv1.PlatformProfile{profile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{TenantID: "acme", DeploymentName: "agent-services", PlatformProfile: compositioncommonv1.Ref{ID: profile.ID, Revision: profile.Revision, Digest: profile.Digest()}}},
	}
	manifest := []byte(fmt.Sprintf(`{"id":%q,"name":"agent","description":"agent","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"configuration":{"scope":"service","applyMode":"restart","schema":{"type":"object","additionalProperties":false,"properties":{"region":{"type":"string"}}}},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, applicationID))
	reader := artifactReader{applicationID + "@1.0.0/stable": {PluginID: applicationID, Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("a", 64), Manifest: manifest}}
	applier := &memoryApplier{}
	catalogPublisher := &memoryCatalogPublisher{}
	publisher, err := New(catalog, reader, applier, catalogPublisher, compositionresolver.Options{}, compositionresolver.Resolve)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := publisher.Preview(context.Background(), "acme", application, 5)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Deployment.Units[0].Replicas != 2 || preview.Digest == "" {
		t.Fatalf("预览不完整: %+v", preview)
	}
	if len(preview.ArtifactReferences) != 1 || preview.ArtifactReferences[0].Ref.PluginID != applicationID || preview.ArtifactReferences[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("预览必须返回由可信多来源读取器解析的精确制品事实: %+v", preview.ArtifactReferences)
	}
	if len(preview.ConfigurationCatalog.Items) != 1 || preview.ConfigurationCatalog.Items[0].PluginID != applicationID || preview.ConfigurationCatalog.Items[0].Origin != deploymentv2.OriginApplication {
		t.Fatalf("预览必须携带从已验证清单生成的配置目录: %+v", preview.ConfigurationCatalog)
	}
	if _, err := publisher.Publish(context.Background(), "acme", application, 5, "stale"); err == nil {
		t.Fatal("过期预览摘要必须拒绝")
	}
	published, err := publisher.Publish(context.Background(), "acme", application, 5, preview.Digest)
	if err != nil {
		t.Fatal(err)
	}
	if published.KVRevision != 7 || applier.key != sharedcontrolplane.DeploymentKey("acme", "agent-services") {
		t.Fatalf("未发布到派生 key: result=%+v key=%s", published, applier.key)
	}
	if catalogPublisher.tenant != "acme" || catalogPublisher.catalog.Digest != published.ConfigurationCatalog.Digest {
		t.Fatalf("Deployment 发布必须同时提交同代配置目录: %+v", catalogPublisher)
	}
	if _, err := publisher.Preview(context.Background(), "other", application, 6); err == nil {
		t.Fatal("认证 tenant 与 composition tenant 不一致必须拒绝")
	}
}
