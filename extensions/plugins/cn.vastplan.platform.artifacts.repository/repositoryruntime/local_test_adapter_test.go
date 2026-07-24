package repositoryruntime

import (
	"context"
	"path/filepath"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
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
