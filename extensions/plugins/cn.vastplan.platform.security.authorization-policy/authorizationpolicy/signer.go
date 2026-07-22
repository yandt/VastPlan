package authorizationpolicy

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	authorizationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/authorization/v1"
)

type SnapshotSigner interface {
	Sign(authorizationv1.PolicySnapshot) (SnapshotPublication, error)
}

type Ed25519Signer struct {
	KeyID   string
	Private ed25519.PrivateKey
}

func (s Ed25519Signer) Sign(snapshot authorizationv1.PolicySnapshot) (SnapshotPublication, error) {
	if s.KeyID == "" || len(s.Private) != ed25519.PrivateKeySize {
		return SnapshotPublication{}, errors.New("Authorization Policy 签名密钥无效")
	}
	canonical, err := authorizationv1.CanonicalPolicySnapshot(snapshot)
	if err != nil {
		return SnapshotPublication{}, err
	}
	signature := ed25519.Sign(s.Private, canonical)
	digest := sha256.Sum256(canonical)
	return SnapshotPublication{Snapshot: authorizationv1.SignedPolicySnapshot{
		Payload:   snapshot,
		Signature: authorizationv1.Signature{Algorithm: "Ed25519", KeyID: s.KeyID, Value: base64.RawURLEncoding.EncodeToString(signature)},
	}, Digest: hex.EncodeToString(digest[:])}, nil
}

type keyDocument struct {
	KeyID      string `json:"keyId"`
	PrivateKey string `json:"privateKey"`
}

func LoadSigner(path string) (Ed25519Signer, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return Ed25519Signer{}, errors.New("Authorization Policy key 必须是规范绝对路径")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return Ed25519Signer{}, errors.New("Authorization Policy key 必须是 owner-only 普通文件")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return Ed25519Signer{}, err
	}
	defer clear(raw)
	var document keyDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		return Ed25519Signer{}, err
	}
	private, err := base64.RawStdEncoding.DecodeString(document.PrivateKey)
	if err != nil || len(private) != ed25519.PrivateKeySize || document.KeyID == "" {
		return Ed25519Signer{}, errors.New("Authorization Policy private key 无效")
	}
	return Ed25519Signer{KeyID: document.KeyID, Private: ed25519.PrivateKey(private)}, nil
}

func WriteSignedSnapshot(path string, snapshot authorizationv1.SignedPolicySnapshot) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || filepath.Ext(path) != ".json" {
		return errors.New("Policy Snapshot 必须写入规范绝对 JSON 路径")
	}
	raw, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".policy-snapshot-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := temporary.Write(append(raw, '\n')); err != nil {
		return err
	}
	if err := errors.Join(temporary.Sync(), temporary.Close()); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	committed = true
	dir, err := os.Open(directory)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}
