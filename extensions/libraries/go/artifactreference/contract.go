// Package artifactreference validates the complete, consumer-owned snapshots
// that protect immutable artifacts from garbage collection.
package artifactreference

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	semver "github.com/Masterminds/semver/v3"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const SchemaVersion = "v1"

const (
	OwnerDeploymentActive = "deployment-active"
	OwnerAssignmentActive = "assignment-active"
	OwnerPortalActivation = "portal-activation"
	OwnerArtifactLock     = "artifact-lock"
	OwnerRollbackHistory  = "rollback-history"
	OwnerSeed             = "seed"
	OwnerLastKnownGood    = "last-known-good"
	OwnerRunnerInstall    = "runner-install"
	OwnerMobileInstall    = "mobile-install"
)

var (
	ownerIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]{0,255}$`)
	purposePattern = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,79}$`)
	pluginPattern  = regexp.MustCompile(`^[a-z][a-z0-9]*(?:\.[a-z0-9][a-z0-9-]*)+$`)
	channelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
)

func Seal(snapshot pluginv1.ArtifactReferenceSnapshot) (pluginv1.ArtifactReferenceSnapshot, error) {
	snapshot.SchemaVersion = SchemaVersion
	snapshot.Digest = ""
	sort.Slice(snapshot.References, func(i, j int) bool {
		return referenceKey(snapshot.References[i]) < referenceKey(snapshot.References[j])
	})
	digest, err := digest(snapshot)
	if err != nil {
		return pluginv1.ArtifactReferenceSnapshot{}, err
	}
	snapshot.Digest = digest
	if err := Validate(snapshot); err != nil {
		return pluginv1.ArtifactReferenceSnapshot{}, err
	}
	return snapshot, nil
}

func Validate(snapshot pluginv1.ArtifactReferenceSnapshot) error {
	if snapshot.SchemaVersion != SchemaVersion || !validOwnerKind(snapshot.OwnerKind) || !ownerIDPattern.MatchString(snapshot.OwnerID) || snapshot.Generation == 0 {
		return errors.New("制品引用快照身份或 generation 无效")
	}
	if snapshot.TTLSeconds != 0 && (snapshot.TTLSeconds < 30 || snapshot.TTLSeconds > 86400) {
		return errors.New("制品引用快照 TTL 必须为 30..86400 秒或 0")
	}
	if len(snapshot.References) > 10000 {
		return errors.New("单个引用快照最多包含 10000 个制品")
	}
	previous := ""
	for _, reference := range snapshot.References {
		key := referenceKey(reference)
		if key <= previous {
			return errors.New("制品引用必须规范排序且不能重复")
		}
		previous = key
		if !pluginPattern.MatchString(reference.Ref.PluginID) || !channelPattern.MatchString(reference.Ref.Channel) || !purposePattern.MatchString(reference.Purpose) {
			return fmt.Errorf("制品引用字段无效: %s", key)
		}
		if _, err := semver.StrictNewVersion(reference.Ref.Version); err != nil {
			return fmt.Errorf("制品引用版本不是精确 SemVer: %w", err)
		}
		if raw, err := hex.DecodeString(reference.SHA256); err != nil || len(raw) != sha256.Size {
			return errors.New("制品引用 SHA-256 无效")
		}
	}
	want, err := digest(snapshot)
	if err != nil || !strings.EqualFold(want, snapshot.Digest) {
		return errors.New("制品引用快照 digest 不匹配")
	}
	return nil
}

func SnapshotKey(tenantID string, snapshot pluginv1.ArtifactReferenceSnapshot) (string, error) {
	if !ownerIDPattern.MatchString(tenantID) {
		return "", errors.New("tenant ID 无效")
	}
	if !validOwnerKind(snapshot.OwnerKind) || !ownerIDPattern.MatchString(snapshot.OwnerID) {
		return "", errors.New("引用 owner 无效")
	}
	return tenantID + "\x00" + snapshot.OwnerKind + "\x00" + snapshot.OwnerID, nil
}

func digest(snapshot pluginv1.ArtifactReferenceSnapshot) (string, error) {
	copy := snapshot
	copy.Digest = ""
	raw, err := json.Marshal(copy)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func referenceKey(reference pluginv1.ArtifactReference) string {
	return reference.Ref.PluginID + "@" + reference.Ref.Version + "/" + reference.Ref.Channel + "\x00" + reference.SHA256 + "\x00" + reference.Purpose
}

func validOwnerKind(kind string) bool {
	switch kind {
	case OwnerDeploymentActive, OwnerAssignmentActive, OwnerPortalActivation, OwnerArtifactLock, OwnerRollbackHistory, OwnerSeed, OwnerLastKnownGood, OwnerRunnerInstall, OwnerMobileInstall:
		return true
	default:
		return false
	}
}
