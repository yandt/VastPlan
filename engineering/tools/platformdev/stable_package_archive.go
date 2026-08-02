package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifacttrust"
)

// hydrateDevelopmentStableArchive makes historical stable refs available to
// development publication validation without widening the Seed inventory.
// The package object is hard-linked from the immutable cache and receives a
// fresh, run-scoped development attestation. Production resolves the same
// history from its remote repository and never calls this helper.
func (r *runtime) hydrateDevelopmentStableArchive() error {
	repositoryRoot := filepath.Join(r.runDir, "repository")
	ledgerPath := stablePackageIdentityLedgerPath(r.options.root)
	count, unavailable, err := hydrateDevelopmentStableArchive(
		repositoryRoot,
		ledgerPath,
		filepath.Join(r.runDir, "secrets", "artifact-signing.pem"),
		filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json"),
	)
	if err != nil {
		return fmt.Errorf("装入开发 stable 历史制品: %w", err)
	}
	if count > 0 {
		log.Printf("已为发布校验装入 %d 个历史 stable 精确制品（包体使用硬链接）", count)
	}
	if unavailable > 0 {
		log.Printf("警告：%d 个未被当前 Seed 使用的历史 stable 对象已不在缓存；实际版本引用仍会在发布/部署时 fail-closed", unavailable)
	}
	return nil
}

func hydrateDevelopmentStableArchive(repositoryRoot, ledgerPath, privateKeyPath, trustPath string) (int, int, error) {
	ledger, err := loadStablePackageIdentityLedger(ledgerPath)
	if err != nil {
		return 0, 0, err
	}
	repository, err := artifactrepository.NewRepository(repositoryRoot)
	if err != nil {
		return 0, 0, err
	}
	privateKey, err := artifactrepository.LoadEd25519PrivateKeyPEM(privateKeyPath)
	if err != nil {
		return 0, 0, err
	}
	trust, err := artifactrepository.LoadTrustStore(trustPath)
	if err != nil {
		return 0, 0, err
	}
	cacheRoot := stablePackageCacheRoot(ledgerPath)
	workspaceStateRoot := filepath.Dir(ledgerPath)
	count := 0
	unavailable := 0
	for _, identity := range ledger.Artifacts {
		if identity.Variant != "" {
			continue
		}
		ref := stableArtifactRef(identity)
		if existing, metadataErr := repository.ReadMetadata(ref); metadataErr == nil {
			if existing.SHA256 != identity.SHA256 {
				return 0, unavailable, fmt.Errorf("运行仓库中的 %s 与 stable 身份账本不一致", stablePackageIdentityLabel(identity))
			}
			continue
		} else if !errors.Is(metadataErr, os.ErrNotExist) {
			return 0, unavailable, metadataErr
		}
		packageBytes, err := loadRecordedStablePackage(workspaceStateRoot, cacheRoot, identity)
		if err != nil {
			if errors.Is(err, errRecordedStablePackageUnavailable) {
				unavailable++
				continue
			}
			return 0, unavailable, err
		}
		artifact, err := artifactrepository.Describe(identity.Channel, packageBytes)
		if err != nil {
			return 0, unavailable, err
		}
		if err := validateStablePackageBytes(identity, artifact, packageBytes); err != nil {
			return 0, unavailable, err
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return 0, unavailable, err
		}
		attestation, err := artifactrepository.SignArtifact(artifact, manifest.Publisher, "local-development", privateKey, time.Now().UTC())
		if err != nil {
			return 0, unavailable, err
		}
		proof, err := json.Marshal(attestation)
		if err != nil {
			return 0, unavailable, err
		}
		if err := trust.VerifyProof(artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: proof}); err != nil {
			return 0, unavailable, err
		}
		cacheObject := stablePackageCacheObject(cacheRoot, identity.SHA256)
		if err := installArchivedStableArtifact(repositoryRoot, artifact, proof, cacheObject); err != nil {
			return 0, unavailable, err
		}
		count++
	}
	return count, unavailable, nil
}

func installArchivedStableArtifact(repositoryRoot string, artifact artifactrepository.Artifact, proof []byte, cacheObject string) error {
	parent := filepath.Join(repositoryRoot, "artifacts", artifact.PluginID, artifact.Version)
	target := filepath.Join(parent, artifact.Channel)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	staging, err := os.MkdirTemp(parent, ".stable-archive-")
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(staging)
		}
	}()
	if err := os.Link(cacheObject, filepath.Join(staging, artifact.Object)); err != nil {
		return fmt.Errorf("硬链接 stable 缓存对象 %s: %w", artifact.SHA256, err)
	}
	metadata, err := json.Marshal(artifact)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "artifact.json"), append(metadata, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(staging, "attestation.json"), append(proof, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(staging, target); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		return nil
	}
	committed = true
	return nil
}
