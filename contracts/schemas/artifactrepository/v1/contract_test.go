package artifactrepositoryv1

import (
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestProfilesSelectExactlyOneRepositoryProtocol(t *testing.T) {
	local, err := ValidateProfile(Profile{
		Version: ProfileVersion, ID: "local-testing", Protocol: ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan/repository.sock", Channels: []string{"testing", "workspace"}, DevelopmentOnly: true,
		Workspace: &WorkspacePolicy{TTLSeconds: 1800, MaxArtifacts: 256},
	})
	if err != nil || len(local.Digest()) != 64 {
		t.Fatalf("local-test Profile 无效: profile=%+v err=%v", local, err)
	}
	remote, err := ValidateProfile(Profile{
		Version: ProfileVersion, ID: "enterprise-primary", Protocol: ProtocolRemote,
		Endpoint: "https://artifacts.example.com", Channels: []string{"candidate", "stable", "testing"},
	})
	if err != nil || len(remote.Digest()) != 64 {
		t.Fatalf("remote Profile 无效: profile=%+v err=%v", remote, err)
	}
	if local.Digest() == remote.Digest() {
		t.Fatal("不同仓库协议不得形成相同 Profile 摘要")
	}
}

func TestReceiptsAndCatalogRemainBoundToExactProfile(t *testing.T) {
	profile, err := ValidateProfile(Profile{
		Version: 1, ID: "local-testing", Protocol: ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan/repository.sock", Channels: []string{"testing", "workspace"}, DevelopmentOnly: true,
		Workspace: &WorkspacePolicy{TTLSeconds: 300, MaxArtifacts: 10},
	})
	if err != nil {
		t.Fatal(err)
	}
	receipt := Receipt{
		SchemaVersion: 1, RepositoryID: profile.ID, Protocol: profile.Protocol, ProfileDigest: profile.Digest(),
		Ref:    pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0-dev.1", Channel: "testing"},
		SHA256: strings.Repeat("a", 64), Revision: 1,
	}
	if err := ValidateReceipt(profile, receipt); err != nil {
		t.Fatal(err)
	}
	if err := ValidateReceiptShape(receipt); err != nil {
		t.Fatal(err)
	}
	snapshot := CatalogSnapshot{
		SchemaVersion: 1, RepositoryID: profile.ID, Protocol: profile.Protocol, ProfileDigest: profile.Digest(),
		Revision: 1, Items: []Receipt{receipt},
	}
	if err := ValidateCatalogSnapshot(profile, snapshot); err != nil {
		t.Fatal(err)
	}

	wrongProtocol := receipt
	wrongProtocol.Protocol = ProtocolRemote
	if err := ValidateReceipt(profile, wrongProtocol); err == nil {
		t.Fatal("回执不得跨协议复用")
	}
	malformed := receipt
	malformed.ProfileDigest = "caller-supplied-label"
	if err := ValidateReceiptShape(malformed); err == nil {
		t.Fatal("Controller 接收前必须拒绝结构不完整的回执")
	}
	workspace := receipt
	workspace.Ref.Channel = "workspace"
	if err := ValidateReceipt(profile, workspace); err == nil {
		t.Fatal("workspace 回执必须携带 lease")
	}
	expiresAt := time.Now().Add(time.Minute)
	workspace.WorkspaceLease = "lease-1"
	workspace.ExpiresAt = &expiresAt
	if err := ValidateReceipt(profile, workspace); err != nil {
		t.Fatal(err)
	}
}

func TestProtocolCapabilitiesCannotDriftAcrossModes(t *testing.T) {
	for _, operation := range []string{OperationReadExact, OperationPublish, OperationCatalogSnapshot} {
		if !Supports(ProtocolLocalTest, operation) || !Supports(ProtocolRemote, operation) {
			t.Fatalf("共享制品语义缺少操作 %s", operation)
		}
	}
	if !Supports(ProtocolLocalTest, OperationExpireWorkspace) || Supports(ProtocolRemote, OperationExpireWorkspace) {
		t.Fatal("workspace 生命周期只能属于 local-test 协议")
	}
	if !Supports(ProtocolLocalTest, OperationImportExact) || Supports(ProtocolRemote, OperationImportExact) {
		t.Fatal("精确导入只能属于 Local Plugin Library")
	}
	if Supports(ProtocolLocalTest, OperationPromote) || !Supports(ProtocolRemote, OperationPromote) {
		t.Fatal("正式晋级只能属于 remote 协议")
	}
}

func TestLocalLibraryCanReadImportedStableButCannotPublishItDirectly(t *testing.T) {
	profile, err := ValidateProfile(Profile{
		Version: ProfileVersion, ID: "local-testing", Protocol: ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan/repository.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	if err := ValidateRef(profile, ref); err != nil {
		t.Fatalf("导入的 stable 应可被本地库读取: %v", err)
	}
	if err := ValidatePublishRef(profile, ref); err == nil {
		t.Fatal("local-test 普通发布不得创建 stable")
	}
}

func TestImportRecordBindsRemoteSourceAndLocalDestination(t *testing.T) {
	local, _ := ValidateProfile(Profile{Version: 1, ID: "local", Protocol: ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	remote, _ := ValidateProfile(Profile{Version: 1, ID: "remote", Protocol: ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	source := Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: ref, SHA256: strings.Repeat("a", 64), Revision: 7}
	destination := Receipt{SchemaVersion: 1, RepositoryID: local.ID, Protocol: local.Protocol, ProfileDigest: local.Digest(), Ref: ref, SHA256: source.SHA256, Revision: 11}
	record := ImportRecord{SchemaVersion: 1, Source: source, Destination: destination, ImportedAt: time.Now().UTC()}
	if err := ValidateImportRecord(remote, local, record); err != nil {
		t.Fatal(err)
	}
	record.Destination.SHA256 = strings.Repeat("b", 64)
	if err := ValidateImportRecord(remote, local, record); err == nil {
		t.Fatal("导入不得改变远端摘要")
	}
}

func TestProfilesRejectProtocolBoundaryCrossing(t *testing.T) {
	tests := []Profile{
		{Version: 1, ID: "local", Protocol: ProtocolLocalTest, Endpoint: "https://localhost", Channels: []string{"testing"}, DevelopmentOnly: true},
		{Version: 1, ID: "remote", Protocol: ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"testing", "workspace"}},
		{Version: 1, ID: "remote", Protocol: ProtocolRemote, Endpoint: "http://repo.example", Channels: []string{"testing"}},
		{Version: 1, ID: "local", Protocol: ProtocolLocalTest, Endpoint: "unix:///tmp/repo.sock", Channels: []string{"testing", "workspace"}, DevelopmentOnly: true},
	}
	for index, profile := range tests {
		if _, err := ValidateProfile(profile); err == nil {
			t.Fatalf("越界 Profile[%d] 必须拒绝: %+v", index, profile)
		}
	}
	if _, err := ParseProfile([]byte(`{"version":1,"id":"x","protocol":"artifact.repository.remote.v1","endpoint":"https://repo.example","channels":["testing"],"extra":true}`)); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Profile 未知字段必须拒绝: %v", err)
	}
}
