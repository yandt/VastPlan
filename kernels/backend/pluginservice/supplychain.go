package pluginservice

import (
	"bytes"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

const attestationSchemaVersion = "v1"

const maximumSigningClockSkew = 5 * time.Minute

// Attestation 把发布者身份绑定到不可变制品元数据。签名覆盖除 Signature 外的全部字段，
// 因而 ref、摘要、大小、对象名、清单和签署时间中任何一项被改写都会验证失败。
type Attestation struct {
	SchemaVersion string    `json:"schemaVersion"`
	Artifact      Artifact  `json:"artifact"`
	Publisher     string    `json:"publisher"`
	KeyID         string    `json:"keyId"`
	Algorithm     string    `json:"algorithm"`
	SignedAt      time.Time `json:"signedAt"`
	Signature     string    `json:"signature"`
}

type signedPayload struct {
	SchemaVersion string    `json:"schemaVersion"`
	Artifact      Artifact  `json:"artifact"`
	Publisher     string    `json:"publisher"`
	KeyID         string    `json:"keyId"`
	Algorithm     string    `json:"algorithm"`
	SignedAt      time.Time `json:"signedAt"`
}

// TrustKey 是一个可轮换、可撤销的发布者公钥。NotBefore/NotAfter 约束签署时间，
// Revoked 用于紧急封禁已泄漏密钥；同一发布者可以并存多个 keyId 完成平滑轮换。
type TrustKey struct {
	Publisher string     `json:"publisher"`
	KeyID     string     `json:"keyId"`
	PublicKey string     `json:"publicKey"`
	NotBefore *time.Time `json:"notBefore,omitempty"`
	NotAfter  *time.Time `json:"notAfter,omitempty"`
	Revoked   bool       `json:"revoked,omitempty"`
}

type TrustDocument struct {
	SchemaVersion string     `json:"schemaVersion"`
	Keys          []TrustKey `json:"keys"`
}

// TrustStore 是只读信任根快照。配置更新通过构造新实例完成，避免验证过程中半更新。
type TrustStore struct {
	keys map[string]ed25519.PublicKey
	meta map[string]TrustKey
}

func trustKeyID(publisher, keyID string) string { return publisher + "\x00" + keyID }

// NewTrustStore 校验信任文档并拒绝重复身份、非法公钥和逆序有效期。
func NewTrustStore(document TrustDocument) (*TrustStore, error) {
	if document.SchemaVersion != attestationSchemaVersion {
		return nil, fmt.Errorf("不支持的信任文档版本 %q", document.SchemaVersion)
	}
	if len(document.Keys) == 0 {
		return nil, errors.New("信任文档至少需要一个发布者公钥")
	}
	store := &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}
	for _, item := range document.Keys {
		item.Publisher = strings.TrimSpace(item.Publisher)
		item.KeyID = strings.TrimSpace(item.KeyID)
		if item.Publisher == "" || item.KeyID == "" {
			return nil, errors.New("信任密钥的 publisher 和 keyId 不能为空")
		}
		if item.NotBefore != nil && item.NotAfter != nil && !item.NotBefore.Before(*item.NotAfter) {
			return nil, fmt.Errorf("信任密钥 %s/%s 的有效期逆序", item.Publisher, item.KeyID)
		}
		raw, err := base64.StdEncoding.DecodeString(item.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("信任密钥 %s/%s 不是合法 Ed25519 公钥", item.Publisher, item.KeyID)
		}
		id := trustKeyID(item.Publisher, item.KeyID)
		if _, exists := store.keys[id]; exists {
			return nil, fmt.Errorf("信任密钥重复: %s/%s", item.Publisher, item.KeyID)
		}
		store.keys[id] = append(ed25519.PublicKey(nil), raw...)
		store.meta[id] = item
	}
	return store, nil
}

func LoadTrustStore(filename string) (*TrustStore, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取制品信任文档: %w", err)
	}
	var document TrustDocument
	if err := decodeJSONStrict(raw, &document); err != nil {
		return nil, fmt.Errorf("解析制品信任文档: %w", err)
	}
	return NewTrustStore(document)
}

// Verify 先确认清单发布者，再验证密钥状态、签署时刻和 Ed25519 签名。
func (s *TrustStore) Verify(attestation Attestation) error {
	return s.verifyAt(attestation, time.Now().UTC())
}

func (s *TrustStore) verifyAt(attestation Attestation, now time.Time) error {
	if s == nil {
		return errors.New("制品信任根未配置")
	}
	if attestation.SchemaVersion != attestationSchemaVersion || attestation.Algorithm != "ed25519" {
		return errors.New("不支持的制品证明版本或签名算法")
	}
	if attestation.SignedAt.IsZero() || attestation.SignedAt.Location() != time.UTC {
		return errors.New("制品证明 signedAt 必须是 UTC 时间")
	}
	if attestation.SignedAt.After(now.Add(maximumSigningClockSkew)) {
		return errors.New("制品证明 signedAt 晚于可信时钟允许范围")
	}
	manifest, err := pluginv1.ParseManifest(attestation.Artifact.Manifest)
	if err != nil {
		return fmt.Errorf("制品证明清单无效: %w", err)
	}
	if manifest.Publisher != attestation.Publisher {
		return fmt.Errorf("签名发布者 %q 与插件清单 publisher %q 不一致", attestation.Publisher, manifest.Publisher)
	}
	id := trustKeyID(attestation.Publisher, attestation.KeyID)
	publicKey, exists := s.keys[id]
	if !exists {
		return fmt.Errorf("发布者密钥不受信任: %s/%s", attestation.Publisher, attestation.KeyID)
	}
	meta := s.meta[id]
	if meta.Revoked {
		return fmt.Errorf("发布者密钥已撤销: %s/%s", attestation.Publisher, attestation.KeyID)
	}
	// 没有可信时间戳服务时，不能用签名者自报的历史时间证明“当时有效”。因此
	// NotBefore/NotAfter 同时约束当前验签时刻；过期密钥签署的历史制品须先重签。
	if meta.NotBefore != nil && now.Before(*meta.NotBefore) {
		return errors.New("发布者密钥尚未生效")
	}
	if meta.NotAfter != nil && now.After(*meta.NotAfter) {
		return errors.New("发布者密钥已经失效；无可信时间戳时不得接受历史签名")
	}
	if meta.NotBefore != nil && attestation.SignedAt.Before(*meta.NotBefore) {
		return errors.New("制品签署时间早于密钥生效时间")
	}
	if meta.NotAfter != nil && attestation.SignedAt.After(*meta.NotAfter) {
		return errors.New("制品签署时间晚于密钥失效时间")
	}
	signature, err := base64.StdEncoding.DecodeString(attestation.Signature)
	if err != nil || len(signature) != ed25519.SignatureSize {
		return errors.New("制品证明签名编码无效")
	}
	payload, err := attestationPayload(attestation)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return errors.New("制品证明签名验证失败")
	}
	return nil
}

// SignArtifact 使用发布者私钥生成确定性 JSON 载荷上的 Ed25519 证明。
func SignArtifact(artifact Artifact, publisher, keyID string, privateKey ed25519.PrivateKey, signedAt time.Time) (Attestation, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return Attestation{}, errors.New("签名私钥不是合法 Ed25519 私钥")
	}
	attestation := Attestation{
		SchemaVersion: attestationSchemaVersion,
		Artifact:      artifact, Publisher: strings.TrimSpace(publisher), KeyID: strings.TrimSpace(keyID),
		Algorithm: "ed25519", SignedAt: signedAt.UTC(),
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return Attestation{}, err
	}
	if attestation.Publisher == "" || attestation.KeyID == "" || manifest.Publisher != attestation.Publisher {
		return Attestation{}, errors.New("签名 publisher/keyId 不能为空且 publisher 必须与清单一致")
	}
	payload, err := attestationPayload(attestation)
	if err != nil {
		return Attestation{}, err
	}
	attestation.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, payload))
	return attestation, nil
}

func attestationPayload(attestation Attestation) ([]byte, error) {
	return json.Marshal(signedPayload{
		SchemaVersion: attestation.SchemaVersion, Artifact: attestation.Artifact,
		Publisher: attestation.Publisher, KeyID: attestation.KeyID,
		Algorithm: attestation.Algorithm, SignedAt: attestation.SignedAt,
	})
}

// LoadEd25519PrivateKeyPEM 读取 PKCS#8 PEM 私钥。私钥文件必须只允许所有者读写，
// 防止构建机上的宽权限文件把签名链退化为共享秘密。
func LoadEd25519PrivateKeyPEM(filename string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, fmt.Errorf("读取签名私钥属性: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("签名私钥权限过宽 %o，要求 0600 或更严格", info.Mode().Perm())
	}
	raw, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("读取签名私钥: %w", err)
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(bytes.TrimSpace(rest)) != 0 || block.Type != "PRIVATE KEY" {
		return nil, errors.New("签名私钥必须是单个 PKCS#8 PRIVATE KEY PEM 块")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("解析签名私钥: %w", err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("签名私钥算法不是 Ed25519")
	}
	return key, nil
}

// MarshalEd25519PrivateKeyPEM 供密钥初始化工具生成标准 PKCS#8 文件。
func MarshalEd25519PrivateKeyPEM(privateKey ed25519.PrivateKey) ([]byte, error) {
	raw, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), nil
}

// SignedRepository 在本地不可变仓库上增加发布者签名强制点。
type SignedRepository struct {
	Local *Repository
	Trust *TrustStore
	mu    sync.Mutex // 证明写入与不可变性检查必须在同一临界区。
}

func (r *SignedRepository) Publish(attestation Attestation, packageBytes []byte) (Artifact, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return Artifact{}, errors.New("签名制品仓库未完整配置")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ValidateArtifact(attestation.Artifact, packageBytes); err != nil {
		return Artifact{}, err
	}
	if err := r.Trust.Verify(attestation); err != nil {
		return Artifact{}, err
	}
	r.Local.mu.Lock()
	published, err := r.Local.publishArtifact(attestation.Artifact, packageBytes)
	r.Local.mu.Unlock()
	if err != nil {
		return Artifact{}, err
	}
	dir, err := r.Local.artifactDir(Ref{PluginID: published.PluginID, Version: published.Version, Channel: published.Channel})
	if err != nil {
		return Artifact{}, err
	}
	raw, err := json.Marshal(attestation)
	if err != nil {
		return Artifact{}, err
	}
	filename := filepath.Join(dir, "attestation.json")
	if existing, readErr := os.ReadFile(filename); readErr == nil {
		if !bytes.Equal(bytes.TrimSpace(existing), raw) {
			return Artifact{}, errors.New("同一不可变制品已经存在不同的签名证明")
		}
		return published, nil
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return Artifact{}, fmt.Errorf("读取既有签名证明: %w", readErr)
	}
	if err := writeFileAtomically(filename, append(raw, '\n'), 0o644); err != nil {
		return Artifact{}, fmt.Errorf("写入签名证明: %w", err)
	}
	return published, nil
}

func (r *SignedRepository) Read(ref Ref) (Artifact, []byte, error) {
	if r == nil || r.Local == nil || r.Trust == nil {
		return Artifact{}, nil, errors.New("签名制品仓库未完整配置")
	}
	artifact, packageBytes, err := r.Local.Read(ref)
	if err != nil {
		return Artifact{}, nil, err
	}
	dir, err := r.Local.artifactDir(ref)
	if err != nil {
		return Artifact{}, nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, "attestation.json"))
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("读取签名证明: %w", err)
	}
	var attestation Attestation
	if err := decodeJSONStrict(raw, &attestation); err != nil {
		return Artifact{}, nil, fmt.Errorf("解析签名证明: %w", err)
	}
	if !sameArtifact(attestation.Artifact, artifact) {
		return Artifact{}, nil, errors.New("签名证明与仓库制品索引不一致")
	}
	if err := r.Trust.Verify(attestation); err != nil {
		return Artifact{}, nil, err
	}
	return artifact, packageBytes, nil
}

func sameArtifact(left, right Artifact) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}

func decodeJSONStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON 只能包含一个顶层值")
		}
		return err
	}
	return nil
}

// TrustDocumentForPublicKeys 生成排序稳定的信任文档，供密钥工具输出后纳入部署配置。
func TrustDocumentForPublicKeys(keys ...TrustKey) TrustDocument {
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Publisher != keys[j].Publisher {
			return keys[i].Publisher < keys[j].Publisher
		}
		return keys[i].KeyID < keys[j].KeyID
	})
	return TrustDocument{SchemaVersion: attestationSchemaVersion, Keys: keys}
}
