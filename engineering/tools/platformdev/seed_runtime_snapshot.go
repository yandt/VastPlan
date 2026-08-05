package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/artifactrepository"
)

const seedRuntimeSnapshotSchema = 3

type seedRuntimeSnapshotMarker struct {
	Schema    int       `json:"schema"`
	Digest    string    `json:"digest"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"createdAt"`
}

type seedRuntimeSnapshotPointer struct {
	Schema int    `json:"schema"`
	Digest string `json:"digest"`
}

func (r *runtime) seedRuntimeSnapshotRoot() string {
	return filepath.Join(r.persistentStateRoot(), "seed-runtime-snapshots")
}

func (r *runtime) seedRuntimeSnapshotPointerPath() string {
	return filepath.Join(r.persistentStateRoot(), "seed-runtime-active.json")
}

func (r *runtime) stageSeedRuntimeSnapshot(sourceRoot, source string) error {
	candidate := filepath.Join(r.runDir, "seed-runtime-candidate")
	if err := os.RemoveAll(candidate); err != nil {
		return err
	}
	if err := copySeedRuntimeSnapshotPayload(sourceRoot, candidate); err != nil {
		return fmt.Errorf("暂存 Seed Runtime 快照: %w", err)
	}
	digest, err := seedRuntimeTreeDigest(candidate)
	if err != nil {
		return err
	}
	marker := seedRuntimeSnapshotMarker{Schema: seedRuntimeSnapshotSchema, Digest: digest, Source: source, CreatedAt: time.Now().UTC()}
	if err := writeAtomicOwnerJSON(filepath.Join(candidate, ".complete.json"), marker); err != nil {
		return err
	}
	if _, err := validateSeedRuntimeSnapshot(candidate); err != nil {
		return fmt.Errorf("验证 Seed Runtime 快照候选: %w", err)
	}
	r.seedSnapshotCandidate = candidate
	return nil
}

func (r *runtime) commitSeedRuntimeSnapshot() error {
	if r.seedSnapshotCandidate == "" {
		return nil
	}
	marker, err := readSeedRuntimeSnapshotMarker(r.seedSnapshotCandidate)
	if err != nil {
		return err
	}
	root := r.seedRuntimeSnapshotRoot()
	if err := os.MkdirAll(root, 0o700); err != nil {
		return err
	}
	target := filepath.Join(root, marker.Digest)
	if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
		if err := os.Rename(r.seedSnapshotCandidate, target); err != nil {
			return fmt.Errorf("提交 Seed Runtime 快照: %w", err)
		}
	} else {
		if err != nil {
			return err
		}
		if _, err := validateSeedRuntimeSnapshot(target); err != nil {
			return fmt.Errorf("同摘要 Seed Runtime 快照目录已存在但无效，拒绝破坏性替换: %w", err)
		}
		if err := os.RemoveAll(r.seedSnapshotCandidate); err != nil {
			return err
		}
	}
	pointer := seedRuntimeSnapshotPointer{Schema: seedRuntimeSnapshotSchema, Digest: marker.Digest}
	if err := writeAtomicOwnerJSON(r.seedRuntimeSnapshotPointerPath(), pointer); err != nil {
		return err
	}
	r.seedSnapshotCandidate = ""
	log.Printf("已提交 Last-Known-Good Seed Runtime 快照 digest=%s", marker.Digest[:12])
	return nil
}

func (r *runtime) restoreSeedRuntimeSnapshot() ([]artifactrepository.Ref, bool, error) {
	raw, err := readOwnerOnlyJSONFile(r.seedRuntimeSnapshotPointerPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var pointer seedRuntimeSnapshotPointer
	if err := json.Unmarshal(raw, &pointer); err != nil || !validSnapshotDigest(pointer.Digest) {
		return nil, false, errors.New("Seed Runtime 活动快照指针无效")
	}
	var refs []artifactrepository.Ref
	switch pointer.Schema {
	case 1:
		refs, err = validateLegacySeedRuntimeSnapshot(filepath.Join(r.seedRuntimeSnapshotRoot(), pointer.Digest))
		r.seedSnapshotMigration = err == nil
	case 2:
		refs, err = validateSeedRuntimeSnapshotV2(filepath.Join(r.seedRuntimeSnapshotRoot(), pointer.Digest))
		r.seedSnapshotMigration = err == nil
	case seedRuntimeSnapshotSchema:
		refs, err = validateSeedRuntimeSnapshot(filepath.Join(r.seedRuntimeSnapshotRoot(), pointer.Digest))
	default:
		return nil, false, errors.New("Seed Runtime 活动快照 schema 不受支持")
	}
	snapshot := filepath.Join(r.seedRuntimeSnapshotRoot(), pointer.Digest)
	if err != nil {
		if errors.Is(err, errSeedRuntimeSnapshotContractIncompatible) {
			return nil, false, fmt.Errorf("活动 Seed Runtime v%d 与当前不可变制品契约不兼容；请显式执行 platform-dev.sh bootstrap --rebuild-seed: %w", pointer.Schema, err)
		}
		return nil, false, fmt.Errorf("活动 Seed Runtime 快照损坏: %w", err)
	}
	if err := materializeSeedRuntimeSnapshot(snapshot, r.runDir, !r.options.applyPlatform); err != nil {
		return nil, false, err
	}
	if r.seedSnapshotMigration && !r.options.applyPlatform {
		if err := materializeMigratedSeedRuntimeCatalog(snapshot, r.runDir); err != nil {
			return nil, false, err
		}
	}
	if r.seedSnapshotMigration {
		log.Printf("复用已验证的 Seed Runtime v%d，并在本次健康启动后迁移快照 v%d digest=%s", pointer.Schema, seedRuntimeSnapshotSchema, pointer.Digest[:12])
	} else {
		log.Printf("普通启动复用 Last-Known-Good Seed Runtime 快照 digest=%s", pointer.Digest[:12])
	}
	return refs, true, nil
}

func readSeedRuntimeSnapshotMarker(root string) (seedRuntimeSnapshotMarker, error) {
	return readSeedRuntimeSnapshotMarkerForSchema(root, seedRuntimeSnapshotSchema)
}

func readSeedRuntimeSnapshotMarkerForSchema(root string, schema int) (seedRuntimeSnapshotMarker, error) {
	raw, err := readOwnerOnlyJSONFile(filepath.Join(root, ".complete.json"))
	if err != nil {
		return seedRuntimeSnapshotMarker{}, err
	}
	var marker seedRuntimeSnapshotMarker
	if json.Unmarshal(raw, &marker) != nil || marker.Schema != schema || !validSnapshotDigest(marker.Digest) || marker.CreatedAt.IsZero() || marker.Source == "" {
		return seedRuntimeSnapshotMarker{}, errors.New("Seed Runtime 快照完成标记无效")
	}
	return marker, nil
}
