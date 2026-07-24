package artifactrepositoryv1

import (
	"strings"
	"testing"
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

func TestProtocolCapabilitiesCannotDriftAcrossModes(t *testing.T) {
	for _, operation := range []string{OperationReadExact, OperationPublish, OperationCatalogSnapshot} {
		if !Supports(ProtocolLocalTest, operation) || !Supports(ProtocolRemote, operation) {
			t.Fatalf("共享制品语义缺少操作 %s", operation)
		}
	}
	if !Supports(ProtocolLocalTest, OperationExpireWorkspace) || Supports(ProtocolRemote, OperationExpireWorkspace) {
		t.Fatal("workspace 生命周期只能属于 local-test 协议")
	}
	if Supports(ProtocolLocalTest, OperationPromote) || !Supports(ProtocolRemote, OperationPromote) {
		t.Fatal("正式晋级只能属于 remote 协议")
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
