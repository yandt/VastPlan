package repositoryruntime

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactstorage"
)

func TestManagerCutsOverMirrorsFinalizesAndReleases(t *testing.T) {
	source, target := migrationVolumes(t, "repository.next")
	trust, privateKey := migrationTrust(t)
	statePath := filepath.Join(t.TempDir(), "state", "repository-migration.json")
	manager, err := Open(source, trust, statePath)
	if err != nil {
		t.Fatal(err)
	}
	first, proof, body := migrationArtifact(t, privateKey, "1.0.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	referenceSnapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerArtifactLock, OwnerID: "deployment/lock-1", Generation: 1, References: []pluginv1.ArtifactReference{{Ref: pluginv1.ArtifactRef{PluginID: first.PluginID, Version: first.Version, Channel: first.Channel}, SHA256: first.SHA256, Purpose: "lock"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, revision, err := manager.PutReferences("tenant-a", "cn.vastplan.platform.infrastructure.deployment-manager", referenceSnapshot, time.Now().UTC()); err != nil || revision != 1 {
		t.Fatalf("reference publish failed: revision=%d err=%v", revision, err)
	}
	seedSnapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerSeed, OwnerID: "seed/primary", Generation: 1, References: []pluginv1.ArtifactReference{{Ref: pluginv1.ArtifactRef{PluginID: "cn.vastplan.bootstrap.only", Version: "1.0.0", Channel: "stable"}, SHA256: strings.Repeat("f", 64), Purpose: "seed"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, revision, err := manager.PutReferences("system", "bootstrap-inventory/primary", seedSnapshot, time.Now().UTC()); err != nil || revision != 2 {
		t.Fatalf("Seed-only ref may be absent from managed catalog: revision=%d err=%v", revision, err)
	}
	request := artifactstorage.VolumeMigrationRequest{MigrationID: "repository.online-001", SourceVolumeID: source.VolumeID, TargetVolumeID: target.VolumeID, Phase: artifactstorage.MigrationPrepare}
	prepared := migrationResult(request, source, target)
	if _, err := manager.Prepare(prepared); err != nil {
		t.Fatal(err)
	}
	request.Phase = artifactstorage.MigrationSync
	syncVolume(t, source.MountPath, target.MountPath)
	synced := migrationResult(request, source, target)
	if _, err := manager.MarkSynced(synced); err != nil {
		t.Fatal(err)
	}
	second, proof, body := migrationArtifact(t, privateKey, "1.1.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	view, err := manager.Cutover(context.Background(), request.MigrationID, 0, func(ctx context.Context) (artifactstorage.VolumeMigrationResult, error) {
		syncVolume(t, source.MountPath, target.MountPath)
		return migrationResult(request, source, target), ctx.Err()
	})
	if err != nil || view.Phase != PhaseObserving || !view.CanFinalize {
		t.Fatalf("cutover failed: view=%+v err=%v", view, err)
	}
	for _, ref := range []struct{ id, version, channel string }{{first.PluginID, first.Version, first.Channel}, {second.PluginID, second.Version, second.Channel}} {
		if _, _, _, err := manager.Read(pluginservice.Ref{PluginID: ref.id, Version: ref.version, Channel: ref.channel}); err != nil {
			t.Fatalf("candidate missing %s: %v", ref.version, err)
		}
	}
	third, proof, body := migrationArtifact(t, privateKey, "1.2.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	referenceSnapshot.Generation = 2
	referenceSnapshot.References = append(referenceSnapshot.References, pluginv1.ArtifactReference{Ref: pluginv1.ArtifactRef{PluginID: third.PluginID, Version: third.Version, Channel: third.Channel}, SHA256: third.SHA256, Purpose: "lock"})
	referenceSnapshot, err = artifactreference.Seal(referenceSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := manager.PutReferences("tenant-a", "cn.vastplan.platform.infrastructure.deployment-manager", referenceSnapshot, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	view, err = manager.Finalize(request.MigrationID, time.Now().UTC())
	if err != nil || view.Phase != PhaseFinalized || view.CanRelease {
		t.Fatalf("finalize must wait for deployment config before release: view=%+v err=%v", view, err)
	}

	restarted, err := Open(prepared.Target, trust, statePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := restarted.Read(pluginservice.Ref{PluginID: third.PluginID, Version: third.Version, Channel: third.Channel}); err != nil {
		t.Fatalf("finalized target did not survive restart: %v", err)
	}
	if revision, snapshots := restarted.References(); revision != 3 || len(snapshots) != 2 || snapshots[0].Value.OwnerKind != artifactreference.OwnerSeed || snapshots[1].Value.Generation != 2 {
		t.Fatalf("reference mirror did not survive restart: revision=%d snapshots=%+v", revision, snapshots)
	}
	releaseRequest, err := restarted.SourceReleaseRequest(request.MigrationID)
	if err != nil {
		t.Fatal(err)
	}
	if releaseRequest.VolumeID != source.VolumeID || releaseRequest.ExpectedHandle != source.Handle {
		t.Fatalf("unexpected release request: %+v", releaseRequest)
	}
	if err := os.Rename(source.MountPath, source.MountPath+".quarantine"); err != nil {
		t.Fatal(err)
	}
	released := artifactstorage.VolumeReleaseResult{MigrationID: request.MigrationID, VolumeID: source.VolumeID, Released: true}
	view, err = restarted.MarkReleased(request.MigrationID, released)
	if err != nil || view.Phase != PhaseReleased {
		t.Fatalf("release failed: view=%+v err=%v", view, err)
	}
	if _, err := os.Stat(source.MountPath); !os.IsNotExist(err) {
		t.Fatalf("source should be quarantined: %v", err)
	}
}

func TestManagerRecoversObservationAndRollsBack(t *testing.T) {
	source, target := migrationVolumes(t, "repository.rollback")
	trust, privateKey := migrationTrust(t)
	statePath := filepath.Join(t.TempDir(), "state", "repository-migration.json")
	manager, err := Open(source, trust, statePath)
	if err != nil {
		t.Fatal(err)
	}
	_, proof, body := migrationArtifact(t, privateKey, "2.0.0")
	if _, err := manager.Publish(proof, body); err != nil {
		t.Fatal(err)
	}
	request := artifactstorage.VolumeMigrationRequest{MigrationID: "repository.online-002", SourceVolumeID: source.VolumeID, TargetVolumeID: target.VolumeID, Phase: artifactstorage.MigrationPrepare}
	prepared := migrationResult(request, source, target)
	if _, err := manager.Prepare(prepared); err != nil {
		t.Fatal(err)
	}
	request.Phase = artifactstorage.MigrationSync
	if _, err := manager.Cutover(context.Background(), request.MigrationID, time.Hour, func(ctx context.Context) (artifactstorage.VolumeMigrationResult, error) {
		syncVolume(t, source.MountPath, target.MountPath)
		return migrationResult(request, source, target), ctx.Err()
	}); err != nil {
		t.Fatal(err)
	}
	recovered, err := Open(source, trust, statePath)
	if err != nil || recovered.Migration().Phase != PhaseObserving {
		t.Fatalf("observation recovery failed: view=%+v err=%v", recovered.Migration(), err)
	}
	if _, err := recovered.Rollback(request.MigrationID); err != nil {
		t.Fatal(err)
	}
	restarted, err := Open(source, trust, statePath)
	if err != nil || restarted.Migration().Phase != PhaseRolledBack {
		t.Fatalf("rollback recovery failed: view=%+v err=%v", restarted.Migration(), err)
	}
}

func TestCutoverRejectsMismatchedProviderReceipt(t *testing.T) {
	source, target := migrationVolumes(t, "repository.mismatch")
	trust, _ := migrationTrust(t)
	manager, err := Open(source, trust, filepath.Join(t.TempDir(), "state", "repository-migration.json"))
	if err != nil {
		t.Fatal(err)
	}
	request := artifactstorage.VolumeMigrationRequest{MigrationID: "repository.online-003", SourceVolumeID: source.VolumeID, TargetVolumeID: target.VolumeID, Phase: artifactstorage.MigrationPrepare}
	if _, err := manager.Prepare(migrationResult(request, source, target)); err != nil {
		t.Fatal(err)
	}
	request.Phase = artifactstorage.MigrationSync
	syncVolume(t, source.MountPath, target.MountPath)
	_, err = manager.Cutover(context.Background(), request.MigrationID, 0, func(context.Context) (artifactstorage.VolumeMigrationResult, error) {
		result := migrationResult(request, source, target)
		result.Source.Handle = "artifact-storage://forged/source"
		return result, nil
	})
	if err == nil || manager.ActiveVolume().VolumeID != source.VolumeID {
		t.Fatalf("mismatched receipt must keep source active: active=%+v err=%v", manager.ActiveVolume(), err)
	}
	manager.recordMigrationError(errors.New("open /private/provider/repository.mismatch: permission denied"))
	view := manager.Migration()
	if view.LastError != publicMigrationError || strings.Contains(view.LastError, "/private/provider") {
		t.Fatalf("administration view must expose only a redacted error: %+v", view)
	}
	raw, err := os.ReadFile(manager.statePath)
	if err != nil || !strings.Contains(string(raw), "/private/provider") {
		t.Fatalf("private durable state must retain diagnostic detail: %s err=%v", raw, err)
	}
}

func migrationVolumes(t *testing.T, targetID string) (artifactstorage.Volume, artifactstorage.Volume) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "volumes")
	for _, path := range []string{root, filepath.Join(root, "repository.primary"), filepath.Join(root, targetID)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	source := artifactstorage.Volume{Handle: "artifact-storage://test/source", ProviderID: "platform.artifacts.storage.file", VolumeID: "repository.primary", AccessMode: "filesystem", MountPath: filepath.Join(root, "repository.primary"), Generation: 1, Ready: true}
	target := artifactstorage.Volume{Handle: "artifact-storage://test/target-" + targetID, ProviderID: source.ProviderID, VolumeID: targetID, AccessMode: "filesystem", MountPath: filepath.Join(root, targetID), Generation: 1, Ready: true}
	return source, target
}

func migrationResult(request artifactstorage.VolumeMigrationRequest, source, target artifactstorage.Volume) artifactstorage.VolumeMigrationResult {
	return artifactstorage.VolumeMigrationResult{MigrationID: request.MigrationID, Phase: request.Phase, Source: source, Target: target, Files: 1, Bytes: 1, Digest: "verified", Ready: true}
}

func syncVolume(t *testing.T, source, target string) {
	t.Helper()
	if err := os.RemoveAll(target); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(target, 0o700); err != nil {
		t.Fatal(err)
	}
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil || path == source {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o700)
		}
		input, err := os.Open(path)
		if err != nil {
			return err
		}
		output, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = input.Close()
			return err
		}
		_, copyErr := io.Copy(output, input)
		return errors.Join(copyErr, input.Close(), output.Close())
	})
	if err != nil {
		t.Fatal(err)
	}
}

func migrationTrust(t *testing.T) (*pluginservice.TrustStore, ed25519.PrivateKey) {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trust, err := pluginservice.NewTrustStore(pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{Publisher: "example", KeyID: "testing", PublicKey: base64.StdEncoding.EncodeToString(publicKey)}))
	if err != nil {
		t.Fatal(err)
	}
	return trust, privateKey
}

func migrationArtifact(t *testing.T, key ed25519.PrivateKey, version string) (pluginservice.Artifact, []byte, []byte) {
	t.Helper()
	directory := t.TempDir()
	manifest := []byte(`{"id":"com.example.migration","name":"Migration","description":"Migration fixture","version":"` + version + `","publisher":"example","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[{"id":"example.migration","service_role":"backend","subcommands":[]}]}}}`)
	if err := os.WriteFile(filepath.Join(directory, "vastplan.plugin.json"), manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(directory, "backend"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "backend", "main"), []byte(version), 0o700); err != nil {
		t.Fatal(err)
	}
	body, parsed, err := pluginservice.PackageDirectory(directory)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := pluginservice.Describe("testing", body)
	if err != nil {
		t.Fatal(err)
	}
	attestation, err := pluginservice.SignArtifact(artifact, parsed.Publisher, "testing", key, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	proof, err := json.Marshal(attestation)
	if err != nil {
		t.Fatal(err)
	}
	return artifact, proof, body
}
