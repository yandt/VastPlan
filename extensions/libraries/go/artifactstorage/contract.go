// Package artifactstorage defines the control-plane contract for provisioning
// storage used by the artifact repository. Providers run on configuration and
// migration paths; artifact object reads/writes do not become cross-plugin RPCs.
package artifactstorage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const CapabilityPrefix = "platform.artifacts.storage."

var identifier = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$`)

const (
	MigrationPrepare = "prepare"
	MigrationSync    = "sync"
	MigrationVerify  = "verify"
)

type ProbeRequest struct {
	VolumeID string `json:"volumeId"`
}

type ProvisionRequest struct {
	VolumeID string `json:"volumeId"`
}

type DescribeRequest struct {
	VolumeID string `json:"volumeId"`
}

// VolumeMigrationRequest is intentionally step based. A caller can retry the same
// migration ID after a deadline without creating a detached privileged worker.
type VolumeMigrationRequest struct {
	MigrationID    string `json:"migrationId"`
	SourceVolumeID string `json:"sourceVolumeId"`
	TargetVolumeID string `json:"targetVolumeId"`
	Phase          string `json:"phase"`
}

type VolumeMigrationResult struct {
	MigrationID string `json:"migrationId"`
	Phase       string `json:"phase"`
	Source      Volume `json:"source"`
	Target      Volume `json:"target"`
	Files       int64  `json:"files"`
	Bytes       int64  `json:"bytes"`
	Digest      string `json:"digest,omitempty"`
	Ready       bool   `json:"ready"`
}

type VolumeReleaseRequest struct {
	MigrationID    string `json:"migrationId"`
	VolumeID       string `json:"volumeId"`
	ExpectedHandle string `json:"expectedHandle"`
}

// VolumeReleaseResult never exposes the quarantine path. File Provider release is
// an atomic retirement into private quarantine, not immediate byte deletion.
type VolumeReleaseResult struct {
	MigrationID string `json:"migrationId"`
	VolumeID    string `json:"volumeId"`
	Released    bool   `json:"released"`
}

// Volume is a non-secret provisioning result. MountPath is consumed by the
// trusted deployment adapter and must not be sent to ordinary Portal users.
type Volume struct {
	Handle     string `json:"handle"`
	ProviderID string `json:"providerId"`
	VolumeID   string `json:"volumeId"`
	AccessMode string `json:"accessMode"`
	MountPath  string `json:"mountPath,omitempty"`
	Endpoint   string `json:"endpoint,omitempty"`
	Generation int64  `json:"generation"`
	Ready      bool   `json:"ready"`
}

type ProbeResult struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

func ValidateProviderID(providerID string) error {
	if !strings.HasPrefix(providerID, CapabilityPrefix) || len(providerID) > 160 || !identifier.MatchString(providerID) {
		return fmt.Errorf("非法制品存储 provider id %q", providerID)
	}
	return nil
}

func ValidateVolumeID(volumeID string) error {
	if len(volumeID) == 0 || len(volumeID) > 80 || !identifier.MatchString(volumeID) {
		return errors.New("volumeId 必须是 1-80 位小写分级标识")
	}
	return nil
}

func ValidateMigrationID(migrationID string) error {
	if len(migrationID) == 0 || len(migrationID) > 96 || !identifier.MatchString(migrationID) {
		return errors.New("migrationId 必须是 1-96 位小写分级标识")
	}
	return nil
}

func ValidateMigrationPhase(phase string) error {
	switch phase {
	case MigrationPrepare, MigrationSync, MigrationVerify:
		return nil
	default:
		return fmt.Errorf("不支持的制品存储迁移阶段 %q", phase)
	}
}
