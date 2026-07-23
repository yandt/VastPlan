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
	now := time.Now().UTC()
	pending, revision, err := store.SubmitPublication(request, "alice", now, now.Add(24*time.Hour))
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
	publicationID, err := store.AuthorizePublication(attestation, now)
	if err != nil || publicationID != pending.ID {
		t.Fatalf("精确批准未授权 stable 发布: id=%s err=%v", publicationID, err)
	}
	tampered := attestation
	tampered.Artifact.SHA256 = "f" + tampered.Artifact.SHA256[1:]
	if _, err := store.AuthorizePublication(tampered, now); err == nil {
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
	now := time.Now().UTC()
	pending, revision, err := store.SubmitPublication(request, "alice", now, now.Add(24*time.Hour))
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
	if _, err := store.AuthorizePublication(attestation, time.Now().UTC()); err == nil {
		t.Fatal("没有批准记录的 stable 发布必须拒绝")
	}
	attestation.Artifact.Channel = "testing"
	if id, err := store.AuthorizePublication(attestation, time.Now().UTC()); err != nil || id != "" {
		t.Fatalf("testing 发布不应进入 stable 审批门: id=%s err=%v", id, err)
	}
}

func TestStablePublicationBindsTestingProvenanceSidecars(t *testing.T) {
	source := pluginv1.ArtifactRef{PluginID: "cn.example.demo", Version: "1.0.0", Channel: "testing"}
	target := source
	target.Channel = "stable"
	provenance, verification := []byte("provenance"), []byte("verification")
	record := Publication{
		ID: "publication", Status: PublicationApproved, Source: source, Target: target, SHA256: "artifact-sha", Publisher: "example", KeyID: "release",
		SourceProvenanceSHA256: digestBytes(provenance), SourceProvenanceVerificationSHA256: digestBytes(verification),
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339Nano),
	}
	store := &Store{entries: map[string]Entry{refKey(source): {Ref: source, SHA256: record.SHA256, Publisher: record.Publisher, KeyID: record.KeyID, LifecycleStatus: LifecycleActive}}, publications: map[string]Publication{record.ID: record}}
	attestation := pluginservice.Attestation{Artifact: pluginv1.Artifact{PluginID: target.PluginID, Version: target.Version, Channel: target.Channel, SHA256: record.SHA256}, Publisher: record.Publisher, KeyID: record.KeyID}
	if id, err := store.AuthorizePublicationWithProvenance(attestation, provenance, verification, time.Now().UTC()); err != nil || id != record.ID {
		t.Fatalf("完全匹配的来源证明应通过: id=%s err=%v", id, err)
	}
	if _, err := store.AuthorizePublicationWithProvenance(attestation, provenance, []byte("reverified"), time.Now().UTC()); err == nil {
		t.Fatal("审批后替换 Verification Record 必须拒绝")
	}
}

func TestPublicationApprovalExpiresBeforeStableAuthorization(t *testing.T) {
	repository, privateKey := testSignedRepository(t, t.TempDir())
	artifact, proof := publishTestArtifact(t, repository, privateKey, "3.0.0")
	store, err := Open(t.TempDir(), repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := store.RecordPublished(artifact, proof, now); err != nil {
		t.Fatal(err)
	}
	request := PublicationRequest{Source: pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"}, TargetChannel: "stable", Reason: "short approval", ExpectedRevision: 0}
	pending, revision, err := store.SubmitPublication(request, "alice", now, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.ApprovePublication(PublicationApprovalRequest{ID: pending.ID, ExpectedRevision: revision}, "bob", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	stable := artifact
	stable.Channel = "stable"
	attestation, err := pluginservice.SignArtifact(stable, "example", "testing", privateKey, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AuthorizePublication(attestation, now.Add(2*time.Hour)); err == nil {
		t.Fatal("过期的批准不得授权 stable 发布")
	}
	page := store.Publications()
	if page.Revision != 3 || page.Items[0].Status != PublicationExpired || page.Items[0].TerminalBy != "system" || page.Items[0].TerminalAt == "" {
		t.Fatalf("审批未收敛到过期终态: %+v", page)
	}
}

func TestPublicationRejectAndCancelEnforceActorBoundaries(t *testing.T) {
	repository, privateKey := testSignedRepository(t, t.TempDir())
	store, err := Open(t.TempDir(), repository)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	submit := func(version string, expected uint64) (Publication, uint64) {
		artifact, proof := publishTestArtifact(t, repository, privateKey, version)
		if _, err := store.RecordPublished(artifact, proof, now); err != nil {
			t.Fatal(err)
		}
		request := PublicationRequest{Source: pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: "testing"}, TargetChannel: "stable", Reason: "release", ExpectedRevision: expected}
		record, revision, err := store.SubmitPublication(request, "alice", now, now.Add(24*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		return record, revision
	}
	rejected, revision := submit("4.0.0", 0)
	transition := PublicationTransitionRequest{ID: rejected.ID, ExpectedRevision: revision, Reason: "risk found"}
	if _, _, err := store.RejectPublication(transition, "alice", now); err == nil {
		t.Fatal("提交人不得驳回自己的申请")
	}
	if record, next, err := store.RejectPublication(transition, "bob", now); err != nil || record.Status != PublicationRejected || next != 2 {
		t.Fatalf("独立审批人驳回失败: record=%+v revision=%d err=%v", record, next, err)
	}
	cancelled, revision := submit("5.0.0", 2)
	cancel := PublicationTransitionRequest{ID: cancelled.ID, ExpectedRevision: revision, Reason: "superseded"}
	if _, _, err := store.CancelPublication(cancel, "bob", now); err == nil {
		t.Fatal("非提交人不得撤销申请")
	}
	if record, next, err := store.CancelPublication(cancel, "alice", now); err != nil || record.Status != PublicationCancelled || next != 4 {
		t.Fatalf("原提交人撤销失败: record=%+v revision=%d err=%v", record, next, err)
	}
}
