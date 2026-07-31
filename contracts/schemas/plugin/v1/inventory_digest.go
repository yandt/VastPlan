package pluginv1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func pluginInventoryDigest(snapshot PluginInventorySnapshot) (string, error) {
	payload := struct {
		SchemaVersion int                   `json:"schemaVersion"`
		Generation    uint64                `json:"generation"`
		SourceDigest  string                `json:"sourceDigest"`
		Plugins       []PluginInventoryItem `json:"plugins"`
	}{snapshot.SchemaVersion, snapshot.Generation, snapshot.SourceDigest, snapshot.Plugins}
	return jsonDigest(payload)
}

func contributionIndexDigest(index ContributionIndexSnapshot) (string, error) {
	payload := struct {
		SchemaVersion   int                   `json:"schemaVersion"`
		Generation      uint64                `json:"generation"`
		InventoryDigest string                `json:"inventoryDigest"`
		Contributions   []IndexedContribution `json:"contributions"`
	}{index.SchemaVersion, index.Generation, index.InventoryDigest, index.Contributions}
	return jsonDigest(payload)
}

func jsonDigest(value any) (string, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

func artifactIdentityKey(identity PluginArtifactIdentity) string {
	return identity.Ref.PluginID + "@" + identity.Ref.Version + "/" + identity.Ref.Channel + "#" + identity.SHA256
}

func isSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
