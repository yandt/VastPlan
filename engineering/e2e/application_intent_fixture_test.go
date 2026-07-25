//go:build e2e

package e2e

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
	repositoryruntime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/repositoryruntime"
)

const (
	p5RootID      = "cn.vastplan.fixture.composition.root"
	p5AuditID     = "cn.vastplan.fixture.composition.audit"
	p5QuotaID     = "cn.vastplan.fixture.composition.quota"
	p5CommonID    = "cn.vastplan.fixture.composition.common"
	p5ConflictID  = "cn.vastplan.fixture.composition.conflict"
	p5ProviderAID = "cn.vastplan.fixture.composition.provider-a"
	p5ProviderBID = "cn.vastplan.fixture.composition.provider-b"
	p5Channel     = "testing"
)

type p5FixtureRepository struct {
	manager    *repositoryruntime.Manager
	trust      *pluginservice.TrustStore
	privateKey ed25519.PrivateKey
	binary     []byte
}

type p5ManifestSpec struct {
	id            string
	version       string
	entry         string
	capability    string
	dependencies  map[string]string
	features      []map[string]any
	configuration map[string]any
}

func newP5FixtureRepository(t *testing.T) *p5FixtureRepository {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := pluginservice.NewTrustStore(pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "vastplan", KeyID: "p5", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	if err != nil {
		t.Fatal(err)
	}
	mount := filepath.Join(t.TempDir(), "repository")
	if err := os.MkdirAll(mount, 0o700); err != nil {
		t.Fatal(err)
	}
	volume := artifactstorage.Volume{
		Handle: "artifact-storage://p5/repository", ProviderID: "platform.artifacts.storage.file", VolumeID: "p5.repository",
		AccessMode: "filesystem", MountPath: mount, Generation: 1, Ready: true,
	}
	manager, err := repositoryruntime.Open(volume, trust, filepath.Join(t.TempDir(), "state", "repository.json"), repositoryruntime.Options{
		SupplyChain: repositoryruntime.SupplyChainPolicy{RequiredSBOMChannels: []string{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	binaryPath := buildPlugin(t, "./engineering/e2e/fixtures/plugins/composition-service")
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	repository := &p5FixtureRepository{manager: manager, trust: trust, privateKey: privateKey, binary: binary}
	for _, spec := range p5BaseManifestSpecs() {
		repository.publish(t, spec)
	}
	return repository
}

func (r *p5FixtureRepository) publishAuditUpgrade(t *testing.T) pluginv1.ArtifactRef {
	t.Helper()
	return r.publish(t, p5AuditSpec("1.2.0", "pipeline-audit-1.2.0"))
}

func (r *p5FixtureRepository) publish(t *testing.T, spec p5ManifestSpec) pluginv1.ArtifactRef {
	t.Helper()
	manifestRaw := p5Manifest(t, spec)
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifestRaw, 0o600); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(directory, "backend", spec.entry)
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, r.binary, 0o700); err != nil {
		t.Fatal(err)
	}
	packageBytes, _, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe(p5Channel, packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	attestation, err := pluginservice.SignArtifact(artifact, "vastplan", "p5", r.privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := json.Marshal(attestation)
	if err != nil {
		t.Fatal(err)
	}
	published, err := r.manager.Publish(proof, packageBytes)
	if err != nil {
		t.Fatalf("发布 P5 制品 %s@%s: %v", spec.id, spec.version, err)
	}
	return pluginv1.ArtifactRef{PluginID: published.PluginID, Version: published.Version, Channel: published.Channel}
}

func (r *p5FixtureRepository) Describe(_ context.Context, request pluginv1.ArtifactPlanningRequest) (pluginv1.ArtifactPlanningResponse, error) {
	return r.manager.DescribePlanning(request)
}

func (r *p5FixtureRepository) Resolve(_ context.Context, request pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	return r.manager.Resolve(request)
}

func (r *p5FixtureRepository) Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error) {
	artifact, body, _, err := r.manager.Read(ref)
	return artifact, body, err
}

func (r *p5FixtureRepository) Fetch(_ context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	artifact, body, proof, provenance, verification, admission, err := r.manager.ReadWithSupplyChain(ref)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	status, err := r.manager.ReadSecurityStatusChain(ref)
	if err != nil {
		status = nil
	}
	return artifacttrust.Envelope{
		Artifact: artifact, PackageBytes: body, Proof: proof, Provenance: provenance,
		ProvenanceVerification: verification, SecurityAdmission: admission, SecurityStatusChain: status,
	}, nil
}

func (r *p5FixtureRepository) SourceName() string { return "p5-signed-repository" }

func p5BaseManifestSpecs() []p5ManifestSpec {
	return []p5ManifestSpec{
		{
			id: p5RootID, version: "1.0.0", entry: "pipeline-root-1.0.0", capability: "fixture.composition.root",
			dependencies: map[string]string{p5CommonID: "^1.0.0"},
			features: []map[string]any{
				{"id": "audit", "title": "审计链", "dependencies": map[string]string{p5AuditID: "^1.0.0"}, "configurationSchema": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"audit_mode": map[string]any{"const": true}, "endpoint": map[string]any{"type": "string", "format": "uri"}}, "required": []string{"audit_mode"}}},
				{"id": "conflict", "title": "冲突链", "dependencies": map[string]string{p5ConflictID: "^1.0.0"}},
				{"id": "provider", "title": "Provider 绑定", "runtimeRequires": []map[string]any{{"capability": "fixture.settings", "scope": "remote", "kind": "strong", "ready": "readiness", "failurePolicy": "fail"}}},
			},
			configuration: map[string]any{
				"scope": "service", "applyMode": "restart",
				"schema":             map[string]any{"type": "object", "additionalProperties": false, "required": []string{"endpoint"}, "properties": map[string]any{"endpoint": map[string]any{"type": "string", "format": "uri"}, "audit_mode": map[string]any{"type": "boolean"}}},
				"managedCredentials": []map[string]any{{"id": "token", "title": "Pipeline token", "purpose": "fixture.composition.root", "required": true}},
			},
		},
		p5AuditSpec("1.1.0", "pipeline-audit-1.1.0"),
		{id: p5QuotaID, version: "1.1.0", entry: "pipeline-quota-1.1.0", capability: "fixture.composition.quota", dependencies: map[string]string{p5CommonID: "^1.0.0"}},
		{id: p5CommonID, version: "1.5.0", entry: "pipeline-common-1.5.0", capability: "fixture.composition.common"},
		{id: p5CommonID, version: "2.1.0", entry: "pipeline-common-2.1.0", capability: "fixture.composition.common"},
		{id: p5ConflictID, version: "1.0.0", entry: "pipeline-conflict-1.0.0", capability: "fixture.composition.conflict", dependencies: map[string]string{p5CommonID: "^2.0.0"}},
		{id: p5ProviderAID, version: "1.0.0", entry: "pipeline-provider-a-1.0.0", capability: "fixture.settings"},
		{id: p5ProviderBID, version: "1.0.0", entry: "pipeline-provider-b-1.0.0", capability: "fixture.settings"},
	}
}

func p5AuditSpec(version, entry string) p5ManifestSpec {
	return p5ManifestSpec{id: p5AuditID, version: version, entry: entry, capability: "fixture.composition.audit", dependencies: map[string]string{p5QuotaID: "^1.0.0"}}
}

func p5Manifest(t *testing.T, spec p5ManifestSpec) []byte {
	t.Helper()
	manifest := map[string]any{
		"id": spec.id, "name": spec.capability, "description": "P5 Application Intent E2E fixture", "version": spec.version,
		"publisher": "vastplan", "engines": map[string]string{"backend": "^0.1 || ^1.0"},
		"runtime": map[string]any{
			"instancePolicy": "active-active", "stateModel": "external-shared", "visibility": "cluster", "routing": "queue", "routingDomain": "application",
			"provides": []map[string]any{{"extensionPoint": "tool.package", "capability": spec.capability, "visibility": "cluster", "routing": "queue", "routingDomain": "application"}},
		},
		"activation": []string{"onStartup"}, "entry": map[string]string{"backend": "backend/" + spec.entry},
		"contributes": map[string]any{"backend": map[string]any{"tools": []map[string]any{{"id": spec.capability, "service_role": "backend", "title": spec.capability, "subcommands": []map[string]any{{"name": "ping", "description": "P5 runtime liveness"}}}}}},
	}
	if len(spec.dependencies) > 0 {
		manifest["dependencies"] = spec.dependencies
	}
	if len(spec.features) > 0 {
		manifest["composition"] = map[string]any{"features": spec.features}
	}
	if spec.configuration != nil {
		manifest["configuration"] = spec.configuration
		manifest["capabilities"] = map[string]any{"kernelServices": []string{"kernel.config.credential-ref"}, "credentials": []string{}, "resources": []string{}}
	}
	raw, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pluginv1.ParseManifest(raw); err != nil {
		t.Fatalf("P5 Manifest 无效 %s@%s: %v\n%s", spec.id, spec.version, err, raw)
	}
	return raw
}
