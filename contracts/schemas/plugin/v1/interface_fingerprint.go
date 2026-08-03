package pluginv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
)

// PublicInterfaceChange describes the structural compatibility relationship
// between two signed manifest public surfaces. It is deliberately narrower
// than a business compatibility promise: a plugin contract may still impose
// semantic rules that cannot be inferred from JSON alone.
type PublicInterfaceChange string

const (
	PublicInterfaceUnchanged PublicInterfaceChange = "unchanged"
	PublicInterfaceAdditive  PublicInterfaceChange = "additive"
	PublicInterfaceBreaking  PublicInterfaceChange = "breaking"
)

// PublicInterfaceFingerprint derives a stable, signed-manifest-bound summary
// of the public surface that consumers can observe. It deliberately excludes
// package bytes, release version, prose metadata, supply-chain evidence and
// frontend module graph hashes: those change for an implementation-only
// release and must not force consumer upgrades.
//
// The value is not an ABI guess from source code. Protocol declarations,
// schemas, capabilities, lifecycle requirements and contribution descriptors
// remain the single source of the semantic surface.
func PublicInterfaceFingerprint(manifest Manifest) (string, error) {
	canonical, err := PublicInterfaceSurface(manifest)
	if err != nil {
		return "", err
	}
	return PublicInterfaceFingerprintFromSurface(canonical)
}

// PublicInterfaceFingerprintFromSurface derives the fingerprint from an
// already canonical public interface projection. Inventory validation uses
// this form so it can prove the stored surface and fingerprint belong together
// without reconstructing a manifest from lossy projections.
func PublicInterfaceFingerprintFromSurface(surface json.RawMessage) (string, error) {
	_, canonical, err := parsePublicInterfaceSurface(surface)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}

// PublicInterfaceSurface returns the canonical signed-manifest projection used
// for upgrade compatibility checks. It must be persisted with a new Inventory
// projection whenever an activation needs to prove an additive upgrade later.
//
// It intentionally excludes release identity, prose, supply-chain evidence and
// module graph bytes. Entry file paths are replaced with their exposed faces.
func PublicInterfaceSurface(manifest Manifest) (json.RawMessage, error) {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}
	var surface map[string]any
	if err := json.Unmarshal(raw, &surface); err != nil {
		return nil, fmt.Errorf("规范化插件公共接口: %w", err)
	}
	for _, field := range []string{"version", "name", "description", "license", "licenseFile", "noticeFile", "supplyChain", "frontendModuleGraphs"} {
		delete(surface, field)
	}
	if entry, ok := surface["entry"].(map[string]any); ok {
		faces := make([]string, 0, len(entry))
		for face, value := range entry {
			if text, ok := value.(string); ok && text != "" {
				faces = append(faces, face)
			}
		}
		sort.Strings(faces)
		surface["entry"] = faces
	}
	canonical, err := json.Marshal(surface)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(canonical), nil
}

// ComparePublicInterfaceSurfaces performs a monotonic structural comparison.
// Existing scalar values must stay identical, existing map keys must remain,
// and every existing array member must remain. New keys or array members are
// additive. This proves a conservative wire-shape property; callers still own
// any protocol-specific semantic compatibility checks.
func ComparePublicInterfaceSurfaces(previous, candidate json.RawMessage) (PublicInterfaceChange, error) {
	previousValue, previousCanonical, err := parsePublicInterfaceSurface(previous)
	if err != nil {
		return "", fmt.Errorf("解析旧公开接口: %w", err)
	}
	candidateValue, candidateCanonical, err := parsePublicInterfaceSurface(candidate)
	if err != nil {
		return "", fmt.Errorf("解析候选公开接口: %w", err)
	}
	if bytes.Equal(previousCanonical, candidateCanonical) {
		return PublicInterfaceUnchanged, nil
	}
	if publicInterfaceContains(candidateValue, previousValue) {
		return PublicInterfaceAdditive, nil
	}
	return PublicInterfaceBreaking, nil
}

func parsePublicInterfaceSurface(raw json.RawMessage) (any, []byte, error) {
	if len(raw) == 0 {
		return nil, nil, fmt.Errorf("公开接口为空")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, nil, err
	}
	object, ok := value.(map[string]any)
	if !ok || object == nil {
		return nil, nil, fmt.Errorf("公开接口必须是 JSON 对象")
	}
	canonical, err := json.Marshal(object)
	if err != nil {
		return nil, nil, err
	}
	return object, canonical, nil
}

func publicInterfaceContains(candidate, previous any) bool {
	switch previousValue := previous.(type) {
	case map[string]any:
		candidateValue, ok := candidate.(map[string]any)
		if !ok {
			return false
		}
		for key, previousChild := range previousValue {
			candidateChild, exists := candidateValue[key]
			if !exists || !publicInterfaceContains(candidateChild, previousChild) {
				return false
			}
		}
		return true
	case []any:
		candidateValue, ok := candidate.([]any)
		if !ok || len(candidateValue) < len(previousValue) {
			return false
		}
		used := make([]bool, len(candidateValue))
		for _, previousChild := range previousValue {
			found := false
			for index, candidateChild := range candidateValue {
				if !used[index] && publicInterfaceContains(candidateChild, previousChild) {
					used[index], found = true, true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(candidate, previous)
	}
}
