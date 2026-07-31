package locallibrary

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

type sourceAdapter struct {
	profile  artifactrepositoryv1.Profile
	receipt  artifactrepositoryv1.Receipt
	envelope artifacttrust.Envelope
	reports  map[string][]byte
}

func (a sourceAdapter) Profile() artifactrepositoryv1.Profile { return a.profile }
func (a sourceAdapter) ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return a.envelope, nil
}
func (a sourceAdapter) Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	panic("not used")
}
func (a sourceAdapter) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	return artifactrepositoryv1.CatalogSnapshot{SchemaVersion: 1, RepositoryID: a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(), Revision: a.receipt.Revision, Items: []artifactrepositoryv1.Receipt{a.receipt}}, nil
}
func (a sourceAdapter) ReadAssessmentReport(_ context.Context, digest string) ([]byte, error) {
	return append([]byte(nil), a.reports[digest]...), nil
}

type destinationAdapter struct {
	profile  artifactrepositoryv1.Profile
	received artifactrepositoryv1.Receipt
	reports  map[string][]byte
}

func (a *destinationAdapter) Profile() artifactrepositoryv1.Profile { return a.profile }
func (*destinationAdapter) ReadExact(context.Context, pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	panic("not used")
}
func (*destinationAdapter) Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	panic("not used")
}
func (*destinationAdapter) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	panic("not used")
}
func (a *destinationAdapter) ImportExact(_ context.Context, source artifactrepositoryv1.Profile, receipt artifactrepositoryv1.Receipt, envelope artifacttrust.Envelope) (artifactrepositoryv1.ImportRecord, error) {
	a.received = receipt
	destination := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: a.profile.ID, Protocol: a.profile.Protocol, ProfileDigest: a.profile.Digest(), Ref: receipt.Ref, SHA256: envelope.Artifact.SHA256, Revision: 9}
	return artifactrepositoryv1.ImportRecord{SchemaVersion: 1, Source: receipt, Destination: destination, ImportedAt: time.Now().UTC()}, nil
}
func (a *destinationAdapter) PutAssessmentReport(_ context.Context, digest string, raw []byte) error {
	if a.reports == nil {
		a.reports = map[string][]byte{}
	}
	a.reports[digest] = append([]byte(nil), raw...)
	return nil
}

func TestImportExactUsesRemoteCatalogIdentity(t *testing.T) {
	remote, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "remote", Protocol: artifactrepositoryv1.ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	local, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "local", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: ref, SHA256: strings.Repeat("a", 64), Revision: 4}
	source := sourceAdapter{profile: remote, receipt: receipt, envelope: artifacttrust.Envelope{Artifact: pluginv1.Artifact{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: receipt.SHA256}}}
	destination := &destinationAdapter{profile: local}
	record, err := ImportExact(context.Background(), source, destination, ref)
	if err != nil {
		t.Fatal(err)
	}
	if destination.received != receipt || record.Source != receipt || record.Destination.Ref != ref {
		t.Fatalf("导入没有保持远端身份: %+v", record)
	}
}

func TestImportExactRejectsCatalogAndObjectDrift(t *testing.T) {
	remote, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "remote", Protocol: artifactrepositoryv1.ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	local, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "local", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: ref, SHA256: strings.Repeat("a", 64), Revision: 4}
	source := sourceAdapter{profile: remote, receipt: receipt, envelope: artifacttrust.Envelope{Artifact: pluginv1.Artifact{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: strings.Repeat("b", 64)}}}
	if _, err := ImportExact(context.Background(), source, &destinationAdapter{profile: local}, ref); err == nil {
		t.Fatal("对象与 Catalog 摘要漂移必须拒绝")
	}
}

func TestImportExactCopiesReferencedAssessmentReportsBeforeArtifact(t *testing.T) {
	remote, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "remote", Protocol: artifactrepositoryv1.ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	local, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "local", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	report := []byte(`{"Results":[]}`)
	reportSum := sha256.Sum256(report)
	reportDigest := hex.EncodeToString(reportSum[:])
	artifactDigest := strings.Repeat("a", 64)
	_, privateKey, _ := ed25519.GenerateKey(rand.Reader)
	now := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	admission, err := artifactassessment.SignAdmission(artifactassessment.AdmissionRecord{
		Evaluation: artifactassessment.Evaluation{
			SubjectSHA256: artifactDigest, SBOMSHA256: strings.Repeat("b", 64),
			Scanner:         artifactassessment.Scanner{ID: "scanner.test", Version: "1.0.0", DatabaseRevision: "2026-08-01"},
			Vulnerabilities: artifactassessment.VulnerabilitySummary{ReportSHA256: reportDigest},
			Licenses:        artifactassessment.LicenseSummary{Allowed: 1, ReportSHA256: reportDigest},
			Decision:        artifactassessment.DecisionPass, EvaluatedAt: now, ExpiresAt: now.Add(24 * time.Hour),
		},
		ProviderID: "security.enterprise", KeyID: "release", PolicyID: "stable-default",
	}, privateKey)
	if err != nil {
		t.Fatal(err)
	}
	admissionRaw, _ := json.Marshal(admission)
	receipt := artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: ref, SHA256: artifactDigest, Revision: 4}
	source := sourceAdapter{
		profile: remote, receipt: receipt, reports: map[string][]byte{reportDigest: report},
		envelope: artifacttrust.Envelope{Artifact: pluginv1.Artifact{PluginID: ref.PluginID, Version: ref.Version, Channel: ref.Channel, SHA256: artifactDigest}, SecurityAdmission: admissionRaw},
	}
	destination := &destinationAdapter{profile: local}
	if _, err := ImportExact(context.Background(), source, destination, ref); err != nil {
		t.Fatal(err)
	}
	if string(destination.reports[reportDigest]) != string(report) {
		t.Fatal("引用的安全评估原始报告未同步")
	}
}

type lockSource struct {
	profile   artifactrepositoryv1.Profile
	snapshot  artifactrepositoryv1.CatalogSnapshot
	envelopes map[pluginv1.ArtifactRef]artifacttrust.Envelope
}

func (s lockSource) Profile() artifactrepositoryv1.Profile { return s.profile }
func (s lockSource) ReadExact(_ context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	return s.envelopes[ref], nil
}
func (lockSource) Publish(context.Context, artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	panic("not used")
}
func (s lockSource) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	return s.snapshot, nil
}
func (lockSource) ResolveLock(context.Context, pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error) {
	panic("not used")
}

func TestImportLockCopiesCompleteExactClosure(t *testing.T) {
	remote, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "remote", Protocol: artifactrepositoryv1.ProtocolRemote, Endpoint: "https://repo.example", Channels: []string{"stable", "testing"}})
	local, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{Version: 1, ID: "local", Protocol: artifactrepositoryv1.ProtocolLocalTest, Endpoint: "unix:///tmp/local.sock", Channels: []string{"stable", "testing"}, DevelopmentOnly: true})
	root := pluginv1.ArtifactRef{PluginID: "cn.example.root", Version: "1.0.0", Channel: "stable"}
	dependency := pluginv1.ArtifactRef{PluginID: "cn.example.dependency", Version: "2.0.0", Channel: "stable"}
	rootSHA, dependencySHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	lock := pluginv1.ArtifactLock{
		SchemaVersion: "v1", RepositoryRevision: 12, Target: "backend", KernelVersion: "0.1.0",
		Roots: []pluginv1.ArtifactRequirement{{PluginID: root.PluginID, Constraint: "=1.0.0", Channel: "stable"}},
		Packages: []pluginv1.ArtifactLockPackage{
			{Ref: dependency, SHA256: dependencySHA, Size: 20, Publisher: "example", KeyID: "release", RepositoryRevision: 10},
			{Ref: root, SHA256: rootSHA, Size: 10, Publisher: "example", KeyID: "release", RepositoryRevision: 12, Dependencies: map[string]string{dependency.PluginID: "^2.0.0"}},
		},
	}
	lock.Digest, _ = pluginv1.ArtifactLockDigest(lock)
	if raw, _ := json.Marshal(lock); pluginv1.ValidateArtifactLock(raw) != nil {
		t.Fatal("测试锁不符合 Schema")
	}
	receipt := func(ref pluginv1.ArtifactRef, digest string, revision uint64) artifactrepositoryv1.Receipt {
		return artifactrepositoryv1.Receipt{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Ref: ref, SHA256: digest, Revision: revision}
	}
	source := lockSource{
		profile:  remote,
		snapshot: artifactrepositoryv1.CatalogSnapshot{SchemaVersion: 1, RepositoryID: remote.ID, Protocol: remote.Protocol, ProfileDigest: remote.Digest(), Revision: 12, Items: []artifactrepositoryv1.Receipt{receipt(dependency, dependencySHA, 10), receipt(root, rootSHA, 12)}},
		envelopes: map[pluginv1.ArtifactRef]artifacttrust.Envelope{
			dependency: {Artifact: pluginv1.Artifact{PluginID: dependency.PluginID, Version: dependency.Version, Channel: dependency.Channel, SHA256: dependencySHA, Size: 20}},
			root:       {Artifact: pluginv1.Artifact{PluginID: root.PluginID, Version: root.Version, Channel: root.Channel, SHA256: rootSHA, Size: 10}},
		},
	}
	destination := &destinationAdapter{profile: local}
	records, err := ImportLock(context.Background(), source, destination, lock)
	if err != nil || len(records) != 2 {
		t.Fatalf("完整依赖闭包导入失败: records=%+v err=%v", records, err)
	}
}
