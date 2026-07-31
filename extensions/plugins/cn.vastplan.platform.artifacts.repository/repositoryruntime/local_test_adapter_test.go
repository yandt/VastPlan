package repositoryruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

func TestLocalTestAdapterUsesManagedRepositoryAndBoundReceipts(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.unused")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, filepath.Join(t.TempDir(), "state", "migration.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan-local-test.sock", Channels: []string{"testing"}, DevelopmentOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewLocalTestAdapter(profile, manager)
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, packageBytes := migrationArtifact(t, privateKey, "11.0.0")
	receipt, err := adapter.Publish(context.Background(), artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof})
	if err != nil {
		t.Fatal(err)
	}
	if err := artifactrepositoryv1.ValidateReceipt(profile, receipt); err != nil || receipt.Revision != 1 {
		t.Fatalf("local-test 回执没有绑定现有 Catalog: receipt=%+v err=%v", receipt, err)
	}
	snapshot, err := adapter.CatalogSnapshot(context.Background())
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0] != receipt {
		t.Fatalf("local-test Catalog 没有复用 Manager 真源: snapshot=%+v err=%v", snapshot, err)
	}
	envelope, err := adapter.ReadExact(context.Background(), receipt.Ref)
	if err != nil || envelope.Artifact.SHA256 != artifact.SHA256 || string(envelope.PackageBytes) != string(packageBytes) {
		t.Fatalf("local-test 精确读取失败: envelope=%+v err=%v", envelope.Artifact, err)
	}
	if _, err := adapter.Publish(context.Background(), artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof, SecurityStatusChain: []byte(`[]`)}); err == nil {
		t.Fatal("发布路径不得覆盖追加式 security status chain")
	}
}

func TestLocalPluginLibraryImportsRemoteStableWithoutAllowingDirectStablePublish(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.local-library")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, filepath.Join(t.TempDir(), "state", "migration.json"), Options{SupplyChain: SupplyChainPolicy{RequiredSBOMChannels: []string{}}})
	if err != nil {
		t.Fatal(err)
	}
	local, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan-local-test.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true,
	})
	remote, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "enterprise", Protocol: artifactrepositoryv1.ProtocolRemote,
		Endpoint: "https://repo.example", Channels: []string{"stable", "testing"},
	})
	adapter, err := NewLocalTestAdapter(local, manager)
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, packageBytes := migrationArtifactForChannel(t, privateKey, "12.0.0", "stable")
	ref := pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	source := artifactrepositoryv1.Receipt{
		SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(),
		Ref: ref, SHA256: artifact.SHA256, Revision: 7,
	}
	envelope := artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof}
	record, err := adapter.ImportExact(context.Background(), remote, source, envelope)
	if err != nil {
		t.Fatal(err)
	}
	if record.Source != source || record.Destination.Ref != ref || record.Destination.SHA256 != artifact.SHA256 {
		t.Fatalf("导入记录没有保持远端身份: %+v", record)
	}
	if _, err := adapter.Publish(context.Background(), envelope); err == nil {
		t.Fatal("普通 local-test 发布不得创建 stable")
	}
	read, err := adapter.ReadExact(context.Background(), ref)
	if err != nil || read.Artifact.SHA256 != artifact.SHA256 {
		t.Fatalf("本地插件库不能读取已导入 stable: %v", err)
	}
	page := manager.Query(catalog.Query{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, Page: 1, PageSize: 2})
	if len(page.Items) != 1 || page.Items[0].ImportSource == nil || *page.Items[0].ImportSource != source {
		t.Fatalf("Catalog 没有保存远端导入来源: %+v", page.Items)
	}
	repeated, err := adapter.ImportExact(context.Background(), remote, source, envelope)
	if err != nil || repeated.Destination.Revision != record.Destination.Revision {
		t.Fatalf("相同远端精确制品导入必须幂等: record=%+v err=%v", repeated, err)
	}
}

func TestLocalTestAdapterPersistsWorkspaceLeaseAndRejectsReceiptDrift(t *testing.T) {
	volume, _ := migrationVolumes(t, "repository.workspace")
	trust, privateKey := migrationTrust(t)
	manager, err := Open(volume, trust, filepath.Join(t.TempDir(), "state", "migration.json"))
	if err != nil {
		t.Fatal(err)
	}
	profile, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "local-testing", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix:///tmp/vastplan-local-test.sock", Channels: []string{"testing", "workspace"}, DevelopmentOnly: true,
		Workspace: &artifactrepositoryv1.WorkspacePolicy{TTLSeconds: 60, MaxArtifacts: 2},
	})
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := NewLocalTestAdapter(profile, manager)
	if err != nil {
		t.Fatal(err)
	}
	artifact, proof, packageBytes := migrationArtifactForChannel(t, privateKey, "11.1.0-dev.1", "workspace")
	receipt, err := adapter.Publish(context.Background(), artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.WorkspaceLease == "" || receipt.ExpiresAt == nil || !receipt.ExpiresAt.After(time.Now().UTC()) {
		t.Fatalf("workspace 发布未签发有界 lease: %+v", receipt)
	}
	restarted, err := NewLocalTestAdapter(profile, manager)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.ValidateReceipt(receipt, time.Now().UTC()); err != nil {
		t.Fatalf("重启后完整回执应保持有效: %v", err)
	}
	stale := receipt
	stale.ProfileDigest = strings.Repeat("f", 64)
	if err := restarted.ValidateReceipt(stale, time.Now().UTC()); err == nil {
		t.Fatal("跨 Profile 回执必须 fail-closed")
	}
	stale = receipt
	stale.WorkspaceLease = "forged"
	if err := restarted.ValidateReceipt(stale, time.Now().UTC()); err == nil {
		t.Fatal("伪造 workspace lease 必须 fail-closed")
	}
	if _, err := restarted.ReadExact(context.Background(), receipt.Ref); err != nil {
		t.Fatalf("活动 workspace lease 应允许精确读取: %v", err)
	}
	withdrawal, err := restarted.WithdrawWorkspace(context.Background(), receipt.Ref)
	if err != nil || withdrawal.Ref != receipt.Ref || withdrawal.WithdrawalRevision <= withdrawal.PublishedRevision {
		t.Fatalf("workspace 撤回失败: record=%+v err=%v", withdrawal, err)
	}
	snapshot, err := restarted.CatalogSnapshot(context.Background())
	if err != nil || len(snapshot.Items) != 0 {
		t.Fatalf("withdrawn 制品不得继续出现在发现快照: snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := restarted.ReadExact(context.Background(), receipt.Ref); err != nil {
		t.Fatalf("撤回后活动代仍应可按精确 lease 读取: %v", err)
	}
	restored, err := restarted.Publish(context.Background(), artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof})
	if err != nil || restored.Ref != receipt.Ref {
		t.Fatalf("同内容源码恢复应重新激活原不可变 ref: receipt=%+v err=%v", restored, err)
	}
	snapshot, err = restarted.CatalogSnapshot(context.Background())
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].Ref != receipt.Ref {
		t.Fatalf("恢复后的 workspace 候选未回到发现快照: snapshot=%+v err=%v", snapshot, err)
	}
}
