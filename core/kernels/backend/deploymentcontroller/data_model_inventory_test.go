package deploymentcontroller

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	recordstorev1 "cdsoft.com.cn/VastPlan/contracts/schemas/recordstore/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
)

type dataModelArtifactReader struct {
	artifacts map[string]pluginv1.Artifact
	packages  map[string][]byte
	calls     *int
}

func (r dataModelArtifactReader) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	if r.calls != nil {
		*r.calls++
	}
	key := ref.PluginID + "@" + ref.Version
	artifact, ok := r.artifacts[key]
	if !ok {
		return pluginv1.Artifact{}, nil, fmt.Errorf("not found: %s", key)
	}
	return artifact, append([]byte(nil), r.packages[key]...), nil
}

func TestTrustedDataModelProjectionBindsVerifiedDeploymentAndReservedConfig(t *testing.T) {
	model := []byte(`{
  "contract":"data.model.v1","id":"example.orders","schemaVersion":1,
  "storage":{"kind":"connection-ref","table":"orders"},
  "fields":[
    {"id":"id","column":"id","type":"uuid","nullable":false,"sensitivity":"internal"},
    {"id":"tenantId","column":"tenant_id","type":"string","nullable":false,"sensitivity":"confidential","maxLength":160}
  ],
  "primaryKey":["id"],"indexes":[],"uniqueConstraints":[],
  "scope":{"tenant":"required","service":"none"},"deletion":{"mode":"hard"}
}`)
	modelDigest := sha256.Sum256(model)
	modelSHA := hex.EncodeToString(modelDigest[:])
	runtimeManifest := []byte(`{
  "id":"com.example.record-runtime","name":"runtime","description":"runtime","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},
  "runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","provides":[{"extensionPoint":"tool.package","capability":"foundation.data.record-store","contractVersion":"1.1.0","visibility":"cluster","routing":"queue"}]},
  "activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"tools":[{"id":"foundation.data.record-store","service_role":"backend","title":"records","subcommands":[]}]}}
}`)
	modelManifest := []byte(fmt.Sprintf(`{
  "id":"com.example.orders","name":"orders","description":"orders","version":"1.0.0","publisher":"example",
  "engines":{"backend":"^1.0"},
  "runtime":{"instancePolicy":"active-active","stateModel":"external-shared","visibility":"cluster","routing":"queue","provides":[{"extensionPoint":"tool.package","capability":"orders","contractVersion":"1.0.0","visibility":"cluster","routing":"queue"}]},
  "activation":["onStartup"],"entry":{"backend":"backend/main"},
  "contributes":{"backend":{"dataModels":[{"id":"example.orders","contractVersion":"1.1.0","path":"data-models/orders.json","sha256":"%s"}],"tools":[{"id":"orders","service_role":"backend","title":"orders","subcommands":[]}]}}
}`, modelSHA))
	artifactSHA := strings.Repeat("a", 64)
	readCalls := 0
	reader := dataModelArtifactReader{
		artifacts: map[string]pluginv1.Artifact{
			"com.example.record-runtime@1.0.0": {PluginID: "com.example.record-runtime", Version: "1.0.0", Channel: "stable", SHA256: artifactSHA, Manifest: runtimeManifest},
			"com.example.orders@1.0.0":         {PluginID: "com.example.orders", Version: "1.0.0", Channel: "stable", SHA256: artifactSHA, Manifest: modelManifest},
		},
		packages: map[string][]byte{
			"com.example.record-runtime@1.0.0": testDataModelPackage(t, map[string][]byte{"backend/main": []byte("runtime")}),
			"com.example.orders@1.0.0":         testDataModelPackage(t, map[string][]byte{"backend/main": []byte("orders"), "data-models/orders.json": model}),
		},
		calls: &readCalls,
	}
	deployment := deploymentv2.Deployment{
		Version: 2, Revision: 7, Metadata: deploymentv1.Metadata{Name: "test", Tenant: "tenant-a"},
		Units: []deploymentv2.ServiceUnit{
			{ID: "runtime", Enabled: true, Plugins: []deploymentv1.PluginRef{{ID: "com.example.record-runtime", Version: "1.0.0", Channel: "stable", SHA256: artifactSHA}}},
			{ID: "orders", Enabled: true, Plugins: []deploymentv1.PluginRef{{ID: "com.example.orders", Version: "1.0.0", Channel: "stable", SHA256: artifactSHA}}},
		},
	}
	projection, err := projectTrustedDataModels(deployment, reader)
	if err != nil {
		t.Fatal(err)
	}
	if projection.request.Generation != 7 || projection.request.InventoryDigest != deployment.Digest() || len(projection.request.Models) != 1 {
		t.Fatalf("可信 DataModel Inventory 身份错误: %+v", projection.request)
	}
	if got := projection.providers["runtime"]; len(got) != 1 || got[0] != "com.example.record-runtime" {
		t.Fatalf("Record Store Provider 投影错误: %v", got)
	}
	cache := &ContractValidationCache{}
	if _, err := cache.projectDataModels(deployment, reader); err != nil {
		t.Fatal(err)
	}
	cachedReads := readCalls
	if _, err := cache.projectDataModels(deployment, reader); err != nil {
		t.Fatal(err)
	}
	if readCalls != cachedReads {
		t.Fatalf("同一 Deployment 的节点/实际态重调度不得重复解包 DataModel: before=%d after=%d", cachedReads, readCalls)
	}
	injected, err := injectTrustedDataModels(deployment.Units[0], projection.request, projection.providers["runtime"])
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := pluginconfig.Parse(injected.Config, []string{"com.example.record-runtime"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := envelope.Plugins["com.example.record-runtime"][recordstorev1.TrustedInventoryConfigKey]; !ok {
		t.Fatal("Controller 未向 Record Store 注入宿主保留 Inventory")
	}

	tampered := deployment.Units[0]
	tampered.Config = map[string]any{"plugins": map[string]any{"com.example.record-runtime": map[string]any{recordstorev1.TrustedInventoryConfigKey: map[string]any{}}}}
	if _, err := injectTrustedDataModels(tampered, projection.request, projection.providers["runtime"]); err == nil {
		t.Fatal("用户配置不得覆盖宿主保留 DataModel Inventory")
	}
}

func testDataModelPackage(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}
