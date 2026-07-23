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
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactprovenance"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

const attestationSchemaVersion = "v1"

const maximumSigningClockSkew = 5 * time.Minute

// Attestation 保留为兼容别名；发布者证明 DTO 的单一真相源在 artifacttrust。
type Attestation = artifacttrust.Attestation

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
	SchemaVersion string                          `json:"schemaVersion"`
	Keys          []TrustKey                      `json:"keys"`
	Provenance    *artifactprovenance.TrustPolicy `json:"provenance,omitempty"`
	Assessment    *artifactassessment.TrustPolicy `json:"assessment,omitempty"`
}

// TrustStore 是只读信任根快照。配置更新通过构造新实例完成，避免验证过程中半更新。
type TrustStore struct {
	keys       map[string]ed25519.PublicKey
	meta       map[string]TrustKey
	provenance *artifactprovenance.Verifier
	assessment *artifactassessment.Verifier
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
	if document.Provenance != nil {
		verifier, err := artifactprovenance.NewVerifier(*document.Provenance)
		if err != nil {
			return nil, fmt.Errorf("来源证明信任策略无效: %w", err)
		}
		store.provenance = verifier
	}
	if document.Assessment != nil {
		verifier, err := artifactassessment.NewVerifier(*document.Assessment)
		if err != nil {
			return nil, fmt.Errorf("安全评估信任策略无效: %w", err)
		}
		store.assessment = verifier
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

// VerifyProof 实现 Node Agent 的内核证明验证边界。Proof 必须严格解析，且其
// Artifact 必须与来源 Envelope 完全一致，来源不能用另一份有效证明替换当前元数据。
func (s *TrustStore) VerifyProof(envelope artifacttrust.Envelope) error {
	if len(bytes.TrimSpace(envelope.Proof)) == 0 {
		return errors.New("制品缺少发布者证明")
	}
	var attestation Attestation
	if err := decodeJSONStrict(envelope.Proof, &attestation); err != nil {
		return fmt.Errorf("解析制品证明: %w", err)
	}
	if !sameArtifact(attestation.Artifact, envelope.Artifact) {
		return errors.New("发布者证明与制品 Envelope 不一致")
	}
	if err := s.Verify(attestation); err != nil {
		return err
	}
	manifest, err := pluginv1.ParseManifest(envelope.Artifact.Manifest)
	if err != nil {
		return err
	}
	_, err = s.provenanceVerifier().Verify(artifactprovenance.ArtifactIdentity{
		PluginID: envelope.Artifact.PluginID, Channel: envelope.Artifact.Channel, Publisher: manifest.Publisher, SHA256: envelope.Artifact.SHA256,
	}, envelope.Provenance, envelope.ProvenanceVerification, time.Now().UTC())
	if err != nil {
		return err
	}
	sbomSHA := ""
	if manifest.SupplyChain != nil && manifest.SupplyChain.SBOM != nil {
		sbomSHA = manifest.SupplyChain.SBOM.SHA256
	}
	identity := artifactassessment.ArtifactIdentity{
		PluginID: envelope.Artifact.PluginID, Channel: envelope.Artifact.Channel, Publisher: manifest.Publisher,
		SHA256: envelope.Artifact.SHA256, SBOMSHA256: sbomSHA,
	}
	statusChain, err := artifactassessment.InspectStatusChain(envelope.SecurityStatusChain)
	if err != nil {
		return err
	}
	if len(statusChain) == 0 {
		record, _, err := s.assessmentVerifier().VerifyAdmission(identity, envelope.SecurityAdmission, time.Now().UTC())
		if err != nil {
			return err
		}
		if record != nil {
			return artifactassessment.EnforceDecision(record.Evaluation)
		}
		return nil
	}
	var previous []byte
	var latest *artifactassessment.StatusRecord
	for index, raw := range statusChain {
		status, _, inspectErr := artifactassessment.InspectStatus(raw)
		if inspectErr != nil {
			return inspectErr
		}
		verifiedAt := status.Evaluation.EvaluatedAt
		if index == len(statusChain)-1 {
			verifiedAt = time.Now().UTC()
		}
		latest, _, err = s.assessmentVerifier().VerifyStatus(identity, envelope.SecurityAdmission, previous, raw, verifiedAt)
		if err != nil {
			return err
		}
		previous = raw
	}
	return artifactassessment.EnforceDecision(latest.Evaluation)
}

func (s *TrustStore) provenanceVerifier() *artifactprovenance.Verifier {
	if s == nil {
		return nil
	}
	return s.provenance
}

func (s *TrustStore) ProvenanceEnabled() bool {
	return s != nil && s.provenance != nil
}

func (s *TrustStore) assessmentVerifier() *artifactassessment.Verifier {
	if s == nil {
		return nil
	}
	return s.assessment
}

func (s *TrustStore) AssessmentEnabled() bool {
	return s != nil && s.assessment != nil
}

func (s *TrustStore) VerifySecurityStatus(artifact Artifact, admissionRaw, previousRaw, statusRaw []byte, now time.Time) (*artifactassessment.StatusRecord, string, error) {
	if s == nil || s.assessment == nil {
		return nil, "", errors.New("安全评估验证器未配置")
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return nil, "", err
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.SBOM == nil {
		return nil, "", errors.New("安全复扫状态要求签名清单绑定 SBOM")
	}
	return s.assessment.VerifyStatus(artifactassessment.ArtifactIdentity{
		PluginID: artifact.PluginID, Channel: artifact.Channel, Publisher: manifest.Publisher,
		SHA256: artifact.SHA256, SBOMSHA256: manifest.SupplyChain.SBOM.SHA256,
	}, admissionRaw, previousRaw, statusRaw, now)
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
