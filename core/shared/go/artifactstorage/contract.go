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

type ProbeRequest struct {
	VolumeID string `json:"volumeId"`
}

type ProvisionRequest struct {
	VolumeID string `json:"volumeId"`
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
