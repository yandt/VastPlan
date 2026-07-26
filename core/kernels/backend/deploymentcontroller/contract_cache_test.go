package deploymentcontroller

import (
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

type countingArtifactReader struct {
	inner contractArtifactReader
	reads int
}

func (r *countingArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	r.reads++
	return r.inner.Read(ref)
}

func TestContractValidationCacheReusesOneDeploymentDigest(t *testing.T) {
	manifest := []byte(`{"id":"com.example.api","name":"api","description":"api","version":"1.0.0","publisher":"example","engines":{"backend":"^1.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	reader := &countingArtifactReader{inner: contractArtifactReader{"com.example.api@1.0.0": {
		PluginID: "com.example.api", Version: "1.0.0", Channel: "stable", SHA256: testArtifactSHA, Manifest: manifest,
	}}}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "prod"},
		Resolution: deploymentv2.Resolution{PluginOrigins: map[string]string{"com.example.api": deploymentv2.OriginApplication}},
		Units:      []deploymentv2.ServiceUnit{{ID: "api", Kind: "service", Enabled: true, ServiceRole: "backend", Replicas: 1, Plugins: []deploymentv1.PluginRef{lockedRef("com.example.api", "1.0.0")}}},
	}
	cache := &ContractValidationCache{}
	for range 2 {
		if err := cache.validate(deployment, map[string][]string{"api": nil}, reader); err != nil {
			t.Fatal(err)
		}
	}
	if reader.reads != 1 {
		t.Fatalf("同一 Deployment digest 只应读取一次 Manifest: reads=%d", reader.reads)
	}
	deployment.Revision++
	if err := cache.validate(deployment, map[string][]string{"api": nil}, reader); err != nil {
		t.Fatal(err)
	}
	if reader.reads != 2 {
		t.Fatalf("新 Deployment digest 必须重新验证: reads=%d", reader.reads)
	}
}
