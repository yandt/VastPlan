// Package authorizationtrust verifies signed authorization snapshots from protected local trust material.
package authorizationtrust

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type FileSnapshotStore struct {
	SnapshotPath string
	TrustPath    string
}

type trustDocument struct {
	Version int        `json:"version"`
	Keys    []trustKey `json:"keys"`
}
type trustKey struct {
	KeyID     string `json:"keyId"`
	PublicKey string `json:"publicKey"`
}

func (s FileSnapshotStore) Load() (authorizationv1.SignedPolicySnapshot, error) {
	if err := securePath(s.SnapshotPath); err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	if err := securePath(s.TrustPath); err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	raw, err := os.ReadFile(s.SnapshotPath)
	if err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	snapshot, err := authorizationv1.ParseSignedPolicySnapshot(raw)
	if err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	trustRaw, err := os.ReadFile(s.TrustPath)
	if err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	var trust trustDocument
	if err := json.Unmarshal(trustRaw, &trust); err != nil || trust.Version != 1 || len(trust.Keys) < 1 || len(trust.Keys) > 16 {
		return authorizationv1.SignedPolicySnapshot{}, errors.New("Authorization Snapshot trust 无效")
	}
	canonical, err := authorizationv1.CanonicalPolicySnapshot(snapshot.Payload)
	if err != nil {
		return authorizationv1.SignedPolicySnapshot{}, err
	}
	signature, err := base64.RawURLEncoding.DecodeString(snapshot.Signature.Value)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return authorizationv1.SignedPolicySnapshot{}, errors.New("Authorization Snapshot signature 编码无效")
	}
	for _, key := range trust.Keys {
		if key.KeyID != snapshot.Signature.KeyID || snapshot.Signature.Algorithm != "Ed25519" {
			continue
		}
		public, decodeErr := base64.RawStdEncoding.DecodeString(key.PublicKey)
		if decodeErr == nil && len(public) == ed25519.PublicKeySize && ed25519.Verify(ed25519.PublicKey(public), canonical, signature) {
			return snapshot, nil
		}
	}
	return authorizationv1.SignedPolicySnapshot{}, errors.New("Authorization Snapshot 签名不受信")
}

func securePath(path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("Authorization Snapshot 文件必须是规范绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("Authorization Snapshot 文件权限不安全")
	}
	return nil
}
