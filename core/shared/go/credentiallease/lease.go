// Package credentiallease defines the encrypted, short-lived material handoff
// between a credential custodian plugin and a trusted kernel adapter.
package credentiallease

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
)

const (
	Version          = 1
	DefaultTTL       = 15 * time.Second
	MaxTTL           = 30 * time.Second
	MaxMaterialBytes = 4 << 20
	keyBytes         = 32
	saltBytes        = 32
)

var rawBase64 = base64.RawURLEncoding

type Request struct {
	Ref                pluginconfig.ManagedCredentialRef `json:"ref"`
	RecipientPublicKey string                            `json:"recipientPublicKey"`
}

type Claims struct {
	TenantID string
	Audience string
	Ref      pluginconfig.ManagedCredentialRef
}

type Envelope struct {
	Version         int                               `json:"version"`
	LeaseID         string                            `json:"leaseId"`
	TenantID        string                            `json:"tenantId"`
	Audience        string                            `json:"audience"`
	Ref             pluginconfig.ManagedCredentialRef `json:"ref"`
	IssuedAtUnixMs  int64                             `json:"issuedAtUnixMs"`
	ExpiresAtUnixMs int64                             `json:"expiresAtUnixMs"`
	SenderPublicKey string                            `json:"senderPublicKey"`
	Salt            string                            `json:"salt"`
	Nonce           string                            `json:"nonce"`
	Ciphertext      string                            `json:"ciphertext"`
}

type aad struct {
	Version         int                               `json:"version"`
	LeaseID         string                            `json:"leaseId"`
	TenantID        string                            `json:"tenantId"`
	Audience        string                            `json:"audience"`
	Ref             pluginconfig.ManagedCredentialRef `json:"ref"`
	IssuedAtUnixMs  int64                             `json:"issuedAtUnixMs"`
	ExpiresAtUnixMs int64                             `json:"expiresAtUnixMs"`
}

// Recipient owns a one-use X25519 private key. Open consumes it even on error.
type Recipient struct {
	mu      sync.Mutex
	private *ecdh.PrivateKey
	used    bool
}

// Discard consumes an unused recipient. Go's crypto/ecdh does not expose a
// private-key destroy primitive, so dropping the last reference is the
// strongest portable cleanup available when a request fails before Open.
func (r *Recipient) Discard() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.used = true
	r.private = nil
	r.mu.Unlock()
}

func NewRequest(ref pluginconfig.ManagedCredentialRef) (Request, *Recipient, error) {
	if err := validateRef(ref); err != nil {
		return Request{}, nil, err
	}
	private, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Request{}, nil, fmt.Errorf("生成 material lease 接收密钥: %w", err)
	}
	return Request{Ref: ref, RecipientPublicKey: rawBase64.EncodeToString(private.PublicKey().Bytes())}, &Recipient{private: private}, nil
}

// ValidateRequest performs all cheap structural checks before a custodian
// spends a Vault/KMS decrypt operation.
func ValidateRequest(request Request) error {
	if err := validateRef(request.Ref); err != nil {
		return err
	}
	raw, err := decodeFixed(request.RecipientPublicKey, keyBytes, "recipient public key")
	zero(raw)
	return err
}

func Seal(request Request, claims Claims, material []byte, now time.Time, ttl time.Duration) (Envelope, error) {
	if err := validateRef(request.Ref); err != nil || request.Ref != claims.Ref {
		return Envelope{}, errors.New("material lease 请求引用与权威声明不匹配")
	}
	if claims.TenantID == "" || claims.Audience == "" || len(material) == 0 || len(material) > MaxMaterialBytes {
		return Envelope{}, errors.New("material lease claims 或 material 无效")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if ttl > MaxTTL {
		return Envelope{}, fmt.Errorf("material lease TTL 超过 %s", MaxTTL)
	}
	recipientRaw, err := decodeFixed(request.RecipientPublicKey, keyBytes, "recipient public key")
	if err != nil {
		return Envelope{}, err
	}
	recipientKey, err := ecdh.X25519().NewPublicKey(recipientRaw)
	zero(recipientRaw)
	if err != nil {
		return Envelope{}, errors.New("material lease recipient public key 无效")
	}
	sender, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Envelope{}, err
	}
	shared, err := sender.ECDH(recipientKey)
	if err != nil {
		return Envelope{}, errors.New("material lease X25519 协商失败")
	}
	defer zero(shared)
	salt := make([]byte, saltBytes)
	if _, err := rand.Read(salt); err != nil {
		return Envelope{}, err
	}
	defer zero(salt)
	key := deriveKey(shared, salt)
	defer zero(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return Envelope{}, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return Envelope{}, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, err
	}
	defer zero(nonce)
	leaseID, err := randomID()
	if err != nil {
		return Envelope{}, err
	}
	now = now.UTC()
	envelope := Envelope{
		Version: Version, LeaseID: leaseID, TenantID: claims.TenantID, Audience: claims.Audience, Ref: claims.Ref,
		IssuedAtUnixMs: now.UnixMilli(), ExpiresAtUnixMs: now.Add(ttl).UnixMilli(),
		SenderPublicKey: rawBase64.EncodeToString(sender.PublicKey().Bytes()), Salt: rawBase64.EncodeToString(salt), Nonce: rawBase64.EncodeToString(nonce),
	}
	associated, err := envelopeAAD(envelope)
	if err != nil {
		return Envelope{}, err
	}
	sealed := gcm.Seal(nil, nonce, material, associated)
	envelope.Ciphertext = rawBase64.EncodeToString(sealed)
	zero(sealed)
	return envelope, nil
}

func (r *Recipient) Open(envelope Envelope, expected Claims, now time.Time) ([]byte, error) {
	if r == nil {
		return nil, errors.New("material lease recipient 不能为空")
	}
	r.mu.Lock()
	if r.used || r.private == nil {
		r.mu.Unlock()
		return nil, errors.New("material lease recipient 已消费")
	}
	r.used = true
	private := r.private
	r.private = nil
	r.mu.Unlock()
	if envelope.Version != Version || envelope.TenantID != expected.TenantID || envelope.Audience != expected.Audience || envelope.Ref != expected.Ref || validateRef(expected.Ref) != nil {
		return nil, errors.New("material lease claims 不匹配")
	}
	nowMs := now.UTC().UnixMilli()
	if envelope.ExpiresAtUnixMs <= envelope.IssuedAtUnixMs || envelope.ExpiresAtUnixMs-envelope.IssuedAtUnixMs > MaxTTL.Milliseconds() || nowMs < envelope.IssuedAtUnixMs-5_000 || nowMs >= envelope.ExpiresAtUnixMs {
		return nil, errors.New("material lease 已过期或时间窗口无效")
	}
	senderRaw, err := decodeFixed(envelope.SenderPublicKey, keyBytes, "sender public key")
	if err != nil {
		return nil, err
	}
	sender, err := ecdh.X25519().NewPublicKey(senderRaw)
	zero(senderRaw)
	if err != nil {
		return nil, errors.New("material lease sender public key 无效")
	}
	shared, err := private.ECDH(sender)
	if err != nil {
		return nil, errors.New("material lease X25519 协商失败")
	}
	defer zero(shared)
	salt, err := decodeFixed(envelope.Salt, saltBytes, "salt")
	if err != nil {
		return nil, err
	}
	defer zero(salt)
	key := deriveKey(shared, salt)
	defer zero(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce, err := decodeFixed(envelope.Nonce, gcm.NonceSize(), "nonce")
	if err != nil {
		return nil, err
	}
	defer zero(nonce)
	ciphertext, err := rawBase64.DecodeString(envelope.Ciphertext)
	if err != nil || len(ciphertext) > MaxMaterialBytes+gcm.Overhead() {
		zero(ciphertext)
		return nil, errors.New("material lease ciphertext 无效")
	}
	defer zero(ciphertext)
	associated, err := envelopeAAD(envelope)
	if err != nil {
		return nil, err
	}
	material, err := gcm.Open(nil, nonce, ciphertext, associated)
	if err != nil {
		return nil, errors.New("material lease 完整性校验失败")
	}
	if len(material) == 0 || len(material) > MaxMaterialBytes {
		zero(material)
		return nil, errors.New("material lease 解封内容无效")
	}
	return material, nil
}

func envelopeAAD(envelope Envelope) ([]byte, error) {
	return json.Marshal(aad{Version: envelope.Version, LeaseID: envelope.LeaseID, TenantID: envelope.TenantID, Audience: envelope.Audience, Ref: envelope.Ref, IssuedAtUnixMs: envelope.IssuedAtUnixMs, ExpiresAtUnixMs: envelope.ExpiresAtUnixMs})
}

func validateRef(ref pluginconfig.ManagedCredentialRef) error {
	if !strings.HasPrefix(ref.Handle, "credential://managed/") || len(ref.Handle) > 256 ||
		ref.Scope != "tenant" || strings.TrimSpace(ref.Owner) == "" || len(ref.Owner) > 160 ||
		strings.TrimSpace(ref.Purpose) == "" || len(ref.Purpose) > 160 || ref.Version < 1 || ref.Name != "" {
		return errors.New("managed CredentialRef 无效")
	}
	return nil
}

func deriveKey(shared, salt []byte) []byte {
	extract := hmac.New(sha256.New, salt)
	_, _ = extract.Write(shared)
	prk := extract.Sum(nil)
	defer zero(prk)
	expand := hmac.New(sha256.New, prk)
	_, _ = expand.Write([]byte("vastplan/material-lease/v1"))
	_, _ = expand.Write([]byte{1})
	return expand.Sum(nil)
}

func decodeFixed(value string, size int, name string) ([]byte, error) {
	raw, err := rawBase64.DecodeString(value)
	if err != nil || len(raw) != size {
		zero(raw)
		return nil, fmt.Errorf("material lease %s 长度无效", name)
	}
	return raw, nil
}

func randomID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	defer zero(raw)
	return "lease-" + hex.EncodeToString(raw), nil
}

func zero(raw []byte) {
	for index := range raw {
		raw[index] = 0
	}
}
