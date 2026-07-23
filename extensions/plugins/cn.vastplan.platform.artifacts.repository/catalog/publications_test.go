package catalog

import (
	"encoding/json"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestPublicationApprovalBindsExactVerifiedTestingArtifact(t *testing.T) {
	repository, privateKey := testSignedRepository(t, t.TempDir())
	artifact, proof := publishTestArtifact(t, repository, privateKey, "1.0.0")
	store, err := Open(t.TempDir(), repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordPublished(artifact, proof, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	request := PublicationRequest{Source: pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"}, TargetChannel: "stable", Reason: "release candidate passed", ExpectedRevision: 0}
	pending, revision, err := store.SubmitPublication(request, "alice", time.Now().UTC())
	if err != nil || revision != 1 || pending.Status != PublicationPending {
		t.Fatalf("提交发布审批失败: record=%+v revision=%d err=%v", pending, revision, err)
	}
	if _, _, err := store.ApprovePublication(PublicationApprovalRequest{ID: pending.ID, ExpectedRevision: revision}, "alice", time.Now().UTC()); err == nil {
		t.Fatal("提交人不得批准自己的发布")
	}
	approved, revision, err := store.ApprovePublication(PublicationApprovalRequest{ID: pending.ID, ExpectedRevision: revision}, "bob", time.Now().UTC())
	if err != nil || revision != 2 || approved.Status != PublicationApproved {
		t.Fatalf("批准发布失败: record=%+v revision=%d err=%v", approved, revision, err)
	}

	stable := artifact
	stable.Channel = "stable"
	attestation, err := pluginservice.SignArtifact(stable, "example", "testing", privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	publicationID, err := store.AuthorizePublication(attestation)
	if err != nil || publicationID != pending.ID {
		t.Fatalf("精确批准未授权 stable 发布: id=%s err=%v", publicationID, err)
	}
	tampered := attestation
	tampered.Artifact.SHA256 = "f" + tampered.Artifact.SHA256[1:]
	if _, err := store.AuthorizePublication(tampered); err == nil {
		t.Fatal("摘要不一致的 stable 发布必须拒绝")
	}

	stableProof, _ := json.Marshal(attestation)
	_, packageBytes, err := repository.Read(pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"})
	if err != nil {
		t.Fatal(err)
	}
	published, err := repository.Publish(attestation, packageBytes)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordPublished(published, stableProof, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkPublicationPublished(publicationID, stableProof, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	page := store.Publications()
	if page.Revision != 3 || len(page.Items) != 1 || page.Items[0].Status != PublicationPublished || page.Items[0].PublishedAttestationSHA256 == "" {
		t.Fatalf("发布审批未原子消费: %+v", page)
	}
	evidence, err := store.Evidence(request.Source)
	if err != nil || evidence.Verification != "verified" || evidence.AttestationSHA256 == "" || len(evidence.Publications) != 1 {
		t.Fatalf("供应链证据无效: evidence=%+v err=%v", evidence, err)
	}
}

func TestApprovedPublicationRecoversAfterObjectCommitCrash(t *testing.T) {
	root := t.TempDir()
	repository, privateKey := testSignedRepository(t, root)
	artifact, proof := publishTestArtifact(t, repository, privateKey, "2.0.0")
	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecordPublished(artifact, proof, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	request := PublicationRequest{Source: pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"}, TargetChannel: "stable", Reason: "recover", ExpectedRevision: 0}
	pending, revision, err := store.SubmitPublication(request, "alice", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApprovePublication(PublicationApprovalRequest{ID: pending.ID, ExpectedRevision: revision}, "bob", time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	stable := artifact
	stable.Channel = "stable"
	attestation, err := pluginservice.SignArtifact(stable, "example", "testing", privateKey, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	_, packageBytes, err := repository.Read(pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Publish(attestation, packageBytes); err != nil {
		t.Fatal(err)
	}

	recovered, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	page := recovered.Publications()
	if len(page.Items) != 1 || page.Items[0].Status != PublicationPublished || page.Items[0].PublishedAttestationSHA256 == "" {
		t.Fatalf("对象提交后的崩溃未收敛: %+v", page)
	}
}

func TestStablePublicationRequiresApproval(t *testing.T) {
	store := &Store{publications: map[string]Publication{}}
	attestation := pluginservice.Attestation{Artifact: pluginv1.Artifact{PluginID: "cn.example.demo", Version: "1.0.0", Channel: "stable", SHA256: "a"}, Publisher: "example", KeyID: "release"}
	if _, err := store.AuthorizePublication(attestation); err == nil {
		t.Fatal("没有批准记录的 stable 发布必须拒绝")
	}
	attestation.Artifact.Channel = "testing"
	if id, err := store.AuthorizePublication(attestation); err != nil || id != "" {
		t.Fatalf("testing 发布不应进入 stable 审批门: id=%s err=%v", id, err)
	}
}
