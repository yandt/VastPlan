package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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
	raw, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	var surface map[string]any
	if err := json.Unmarshal(raw, &surface); err != nil {
		return "", fmt.Errorf("规范化插件公共接口: %w", err)
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
		return "", err
	}
	digest := sha256.Sum256(canonical)
	return hex.EncodeToString(digest[:]), nil
}
