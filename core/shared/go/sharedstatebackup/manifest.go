// Package sharedstatebackup implements signed, native JetStream backups for
// the Shared State bucket. Snapshot bytes remain opaque; a value-free logical
// digest and domain validators prove the restored latest state.
package sharedstatebackup

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	ManifestFormat  = "vastplan.shared-state-backup.v1"
	SignatureFormat = "vastplan.shared-state-backup-signature.v1"
	TrustFormat     = "vastplan.shared-state-backup-trust.v1"

	ManifestFilename  = "manifest.json"
	SignatureFilename = "manifest.sig.json"
	SnapshotFilename  = "stream.snapshot"

	MaxManifestBytes   = 1 << 20
	MaxSignatureBytes  = 64 << 10
	MaxTrustBytes      = 64 << 10
	MaxPrivateKeyBytes = 64 << 10
)

type Manifest struct {
	Format       string             `json:"format"`
	CreatedAt    time.Time          `json:"createdAt"`
	Bucket       string             `json:"bucket"`
	Stream       string             `json:"stream"`
	Snapshot     SnapshotDescriptor `json:"snapshot"`
	Logical      LogicalSummary     `json:"logical"`
	StreamConfig json.RawMessage    `json:"streamConfig"`
	StreamState  json.RawMessage    `json:"streamState"`
	Validations  []ValidationResult `json:"validations"`
}

type SnapshotDescriptor struct {
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type LogicalSummary struct {
	Entries     uint64 `json:"entries"`
	ValueBytes  uint64 `json:"valueBytes"`
	MaxRevision uint64 `json:"maxRevision"`
	Digest      string `json:"digest"`
}

type ValidationResult struct {
	Name     string            `json:"name"`
	Counters map[string]uint64 `json:"counters,omitempty"`
}

type ManifestSignature struct {
	Format    string `json:"format"`
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"keyId"`
	Value     string `json:"value"`
}

type TrustDocument struct {
	Format string     `json:"format"`
	Keys   []TrustKey `json:"keys"`
}

type TrustKey struct {
	KeyID     string `json:"keyId"`
	Algorithm string `json:"algorithm"`
	PublicKey string `json:"publicKey"`
}

func MarshalManifest(value Manifest) ([]byte, error) {
	if err := value.Validate(); err != nil {
		return nil, err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(raw)+1 > MaxManifestBytes {
		return nil, errors.New("Shared State 备份清单超过大小上限")
	}
	return append(raw, '\n'), nil
}

func ParseManifest(raw []byte) (Manifest, error) {
	var value Manifest
	if err := decodeStrictJSON(raw, &value); err != nil {
		return value, fmt.Errorf("解析 Shared State 备份清单: %w", err)
	}
	return value, value.Validate()
}

func (value Manifest) Validate() error {
	if value.Format != ManifestFormat || value.CreatedAt.IsZero() || !safeToken(value.Bucket) || value.Stream != "KV_"+value.Bucket {
		return errors.New("Shared State 备份清单身份无效")
	}
	if !validDigest(value.Snapshot.SHA256) || value.Snapshot.Bytes < 1 || !validDigest(value.Logical.Digest) {
		return errors.New("Shared State 备份清单摘要无效")
	}
	if !validJSONObject(value.StreamConfig) || !validJSONObject(value.StreamState) {
		return errors.New("Shared State 备份清单缺少 Stream 配置或状态")
	}
	var config struct {
		Name     string   `json:"name"`
		Subjects []string `json:"subjects"`
	}
	var state struct {
		LastSeq uint64 `json:"last_seq"`
	}
	if json.Unmarshal(value.StreamConfig, &config) != nil || config.Name != value.Stream || len(config.Subjects) != 1 || config.Subjects[0] != "$KV."+value.Bucket+".>" || json.Unmarshal(value.StreamState, &state) != nil || value.Logical.MaxRevision > state.LastSeq {
		return errors.New("Shared State 备份 Stream 身份或 revision 边界无效")
	}
	seen := map[string]struct{}{}
	for _, result := range value.Validations {
		if !safeName(result.Name) {
			return errors.New("Shared State 备份验证器名称无效")
		}
		if _, exists := seen[result.Name]; exists {
			return errors.New("Shared State 备份验证器重复")
		}
		seen[result.Name] = struct{}{}
		for key := range result.Counters {
			if !safeName(key) {
				return errors.New("Shared State 备份验证计数器无效")
			}
		}
	}
	return nil
}

func SignManifest(raw []byte, keyID string, private ed25519.PrivateKey) (ManifestSignature, error) {
	if !safeName(keyID) || len(private) != ed25519.PrivateKeySize {
		return ManifestSignature{}, errors.New("Shared State 备份签名输入无效")
	}
	signature := ed25519.Sign(private, raw)
	return ManifestSignature{Format: SignatureFormat, Algorithm: "Ed25519", KeyID: keyID, Value: base64.RawURLEncoding.EncodeToString(signature)}, nil
}

func VerifyManifest(raw []byte, signature ManifestSignature, trust TrustDocument) error {
	if signature.Format != SignatureFormat || signature.Algorithm != "Ed25519" || !safeName(signature.KeyID) {
		return errors.New("Shared State 备份签名信封无效")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(signature.Value)
	if err != nil || len(decoded) != ed25519.SignatureSize {
		return errors.New("Shared State 备份签名编码无效")
	}
	if trust.Format != TrustFormat || len(trust.Keys) == 0 {
		return errors.New("Shared State 备份信任文档无效")
	}
	for _, key := range trust.Keys {
		if key.KeyID != signature.KeyID || key.Algorithm != "Ed25519" {
			continue
		}
		public, decodeErr := base64.RawURLEncoding.DecodeString(key.PublicKey)
		if decodeErr == nil && len(public) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(public), raw, decoded) {
			return nil
		}
	}
	return errors.New("Shared State 备份签名不受信或验证失败")
}

func ManifestSHA256(raw []byte) string {
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func decodeStrictJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("JSON 包含尾随数据")
	}
	return nil
}

func validJSONObject(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) >= 2 && trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}' && json.Valid(trimmed)
}

func validDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func safeToken(value string) bool {
	return value != "" && !strings.ContainsAny(value, " \t\r\n.*>/\\")
}

func safeName(value string) bool {
	if value == "" || len(value) > 160 || value != strings.TrimSpace(value) {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}
