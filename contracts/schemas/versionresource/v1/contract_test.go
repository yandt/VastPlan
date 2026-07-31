package versionresourcev1_test

import (
	"encoding/json"
	"strings"
	"testing"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
)

func TestEnvironmentProfileKeepsProviderChoiceOutOfConsumerBindings(t *testing.T) {
	profile := resourcev1.EnvironmentProfile{
		Protocol: resourcev1.Protocol, ID: "platform-development", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{
			ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: "version.resource.json.v1",
			AllowedModes: []string{resourcev1.ModeOverlay, resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionDomainHot,
		}},
		Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 64, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 64 << 20},
	}
	if err := resourcev1.ValidateEnvironmentProfile(profile); err != nil {
		t.Fatal(err)
	}
	if digest, err := resourcev1.EnvironmentDigest(profile); err != nil || len(digest) != 64 {
		t.Fatalf("环境 Profile 未形成稳定摘要: %s %v", digest, err)
	}
	reordered := profile
	reordered.Bindings = append([]resourcev1.ResourceBinding(nil), profile.Bindings...)
	reordered.Bindings[0].AllowedModes = []string{resourcev1.ModeSnapshot, resourcev1.ModeOverlay}
	left, _ := resourcev1.EnvironmentDigest(profile)
	right, _ := resourcev1.EnvironmentDigest(reordered)
	if left != right {
		t.Fatal("语义相同的模式顺序不得改变 Environment digest")
	}
	raw, _ := json.Marshal(profile)
	if strings.Contains(string(raw), "provider") || strings.Contains(string(raw), "endpoint") || strings.Contains(string(raw), "credential") {
		t.Fatalf("资源绑定不得允许消费者选择存储或凭证: %s", raw)
	}
}

func TestSnapshotValidationSeparatesJSONFromFileManifest(t *testing.T) {
	jsonSnapshot := resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(`{"enabled":true}`)}
	if err := resourcev1.ValidateSnapshot(jsonSnapshot, 1024); err != nil {
		t.Fatal(err)
	}
	canonical, err := resourcev1.CanonicalContent(jsonSnapshot, 1024)
	if err != nil || string(canonical) != `{"enabled":true}` {
		t.Fatalf("JSON 资源规范化失败: %s %v", canonical, err)
	}
	files := resourcev1.Snapshot{Kind: resourcev1.ContentFiles, MediaType: resourcev1.FilesManifestMediaType, Files: []resourcev1.FileEntry{
		{Path: "README.md", Digest: strings.Repeat("a", 64), Size: 12, Mode: 0o644, MediaType: "text/markdown; charset=utf-8"},
		{Path: "src/main.ts", Digest: strings.Repeat("b", 64), Size: 42, Mode: 0o644, MediaType: "text/typescript; charset=utf-8"},
	}}
	if err := resourcev1.ValidateSnapshot(files, 1024); err != nil {
		t.Fatal(err)
	}
	if digest, err := resourcev1.SnapshotDigest(files, 1024); err != nil || len(digest) != 64 {
		t.Fatalf("文件清单未形成稳定版本摘要: %s %v", digest, err)
	}
	files.Files[1].Path = "../escape"
	if err := resourcev1.ValidateSnapshot(files, 1024); err == nil {
		t.Fatal("文件清单必须拒绝目录穿越")
	}
	files.Files[1].Path = "src/main.ts"
	files.Files[1].MediaType = "Text/Plain"
	if err := resourcev1.ValidateSnapshot(files, 1024); err == nil {
		t.Fatal("文件清单必须拒绝非规范媒体类型")
	}
}

func TestJSONAdapterCannotAccidentallyMakeGitTheDefault(t *testing.T) {
	descriptor := resourcev1.AdapterDescriptor{
		Protocol: resourcev1.Protocol, ID: "version.resource.json.v1", Version: "1.0.0", ContentKind: resourcev1.ContentJSON,
		SupportedModes: []string{resourcev1.ModeSnapshot, resourcev1.ModeGit}, DefaultMode: resourcev1.ModeSnapshot,
		MaxSnapshotBytes: 1 << 20, SecretPolicy: resourcev1.SecretPolicyCredentialRefsOnly,
		ConfigurationSchema: json.RawMessage(`{"type":"object"}`),
	}
	if err := resourcev1.ValidateAdapterDescriptor(descriptor); err == nil {
		t.Fatal("JSON Adapter 不得启用重型 Git 模式")
	}
}

func TestAdapterMustDeclareNormalizeCapability(t *testing.T) {
	descriptor := resourcev1.AdapterDescriptor{
		Protocol: resourcev1.Protocol, ID: "version.resource.blob.v1", Version: "1.0.0", ContentKind: resourcev1.ContentFiles,
		SupportedModes: []string{resourcev1.ModeOverlay}, DefaultMode: resourcev1.ModeOverlay,
		MaxSnapshotBytes: 1 << 20, SecretPolicy: resourcev1.SecretPolicyForbidden,
		ConfigurationSchema: json.RawMessage(`{"type":"object"}`),
	}
	if err := resourcev1.ValidateAdapterDescriptor(descriptor); err == nil {
		t.Fatal("Adapter 必须显式声明 normalize 强制能力")
	}
	descriptor.Capabilities.Normalize = true
	if err := resourcev1.ValidateAdapterDescriptor(descriptor); err != nil {
		t.Fatal(err)
	}
}

func TestAdapterResultsAreDigestBoundAndPathOpaque(t *testing.T) {
	snapshot := resourcev1.Snapshot{Kind: resourcev1.ContentJSON, MediaType: "application/json", JSON: json.RawMessage(`{"enabled":true}`)}
	digest, err := resourcev1.SnapshotDigest(snapshot, 1024)
	if err != nil {
		t.Fatal(err)
	}
	if err := resourcev1.ValidateAdapterNormalizeResult(resourcev1.AdapterNormalizeResult{Snapshot: snapshot, Digest: digest}, 1024); err != nil {
		t.Fatal(err)
	}
	changes := resourcev1.AdapterDiffResult{ChangedPaths: []string{"/enabled"}, Summary: resourcev1.ChangeSummary{Modified: 1, Total: 1}}
	if err := resourcev1.ValidateAdapterDiffResult(changes); err != nil {
		t.Fatal(err)
	}
	materialization := resourcev1.MaterializationRef{ID: "mat_1234567890abcdef", Digest: digest, Size: 12}
	if err := resourcev1.ValidateMaterializationRef(materialization); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(materialization.ID, "/") {
		t.Fatal("物化引用不得暴露宿主本地路径")
	}
}
