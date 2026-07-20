package addressing

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nkeys"
	"google.golang.org/protobuf/proto"

	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

const (
	transportPublicKeyHeader = "VastPlan-Identity"
	transportTimestampHeader = "VastPlan-Timestamp"
	transportNonceHeader     = "VastPlan-Nonce"
	transportSignatureHeader = "VastPlan-Signature"
	defaultTransportSkew     = 5 * time.Minute
)

// TransportIdentity 是跨节点传输层认可的工作负载身份。AllowDelegation 仅授予
// 已验证边缘/节点代用户传递 Principal/Caller 的能力；未授予时接收端会清空
// 自报 Principal，并把 Caller 重建为该工作负载。
type TransportIdentity struct {
	Name                string   `json:"name"`
	Role                string   `json:"role"`
	PublicKey           string   `json:"publicKey"`
	TenantID            string   `json:"tenantId,omitempty"`
	NodeID              string   `json:"nodeId,omitempty"`
	ServiceRoles        []string `json:"serviceRoles,omitempty"`
	LogicalServices     []string `json:"logicalServices,omitempty"`
	AllowedCapabilities []string `json:"allowedCapabilities"`
	AllowGlobal         bool     `json:"allowGlobal,omitempty"`
	AllowDelegation     bool     `json:"allowDelegation,omitempty"`
}

type TransportTrustDocument struct {
	Version    int                 `json:"version"`
	Identities []TransportIdentity `json:"identities"`
}

// TransportSecurity 使用现有 NKey 对传输信封签名，并按本地信任文档验证远端。
// replay 表只保留一个时间窗，避免无界增长。
type TransportSecurity struct {
	pair      nkeys.KeyPair
	publicKey string
	self      TransportIdentity
	trusted   map[string]TransportIdentity
	maxSkew   time.Duration

	replayMu sync.Mutex
	replay   map[string]time.Time
}

func LoadTransportSecurity(seedFile, trustFile string) (*TransportSecurity, error) {
	if seedFile == "" || trustFile == "" {
		return nil, errors.New("传输安全必须同时配置 NKey seed 和身份信任文档")
	}
	info, err := os.Stat(seedFile)
	if err != nil {
		return nil, fmt.Errorf("读取传输 NKey seed 属性: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("传输 NKey seed 权限过宽 %o，要求 0600 或更严格", info.Mode().Perm())
	}
	seed, err := os.ReadFile(seedFile)
	if err != nil {
		return nil, fmt.Errorf("读取传输 NKey seed: %w", err)
	}
	rawTrust, err := os.ReadFile(trustFile)
	if err != nil {
		return nil, fmt.Errorf("读取传输身份信任文档: %w", err)
	}
	var document TransportTrustDocument
	if err := json.Unmarshal(rawTrust, &document); err != nil {
		return nil, fmt.Errorf("解析传输身份信任文档: %w", err)
	}
	return newTransportSecurity(seed, document)
}

func newTransportSecurity(seed []byte, document TransportTrustDocument) (*TransportSecurity, error) {
	if document.Version != 1 || len(document.Identities) == 0 {
		return nil, errors.New("传输身份信任文档必须是 version=1 且至少包含一个身份")
	}
	pair, err := nkeys.FromSeed([]byte(strings.TrimSpace(string(seed))))
	if err != nil {
		return nil, fmt.Errorf("加载传输 NKey: %w", err)
	}
	publicKey, err := pair.PublicKey()
	if err != nil {
		pair.Wipe()
		return nil, fmt.Errorf("读取传输 NKey 公钥: %w", err)
	}
	security := &TransportSecurity{
		pair: pair, publicKey: publicKey, trusted: map[string]TransportIdentity{},
		maxSkew: defaultTransportSkew, replay: map[string]time.Time{},
	}
	for _, identity := range document.Identities {
		if identity.Name == "" || identity.Role == "" || !nkeys.IsValidPublicUserKey(identity.PublicKey) {
			pair.Wipe()
			return nil, fmt.Errorf("传输信任身份字段非法: %+v", identity)
		}
		if len(identity.AllowedCapabilities) == 0 {
			pair.Wipe()
			return nil, fmt.Errorf("传输信任身份 %s 必须声明 allowedCapabilities", identity.Name)
		}
		if _, exists := security.trusted[identity.PublicKey]; exists {
			pair.Wipe()
			return nil, fmt.Errorf("传输信任公钥重复: %s", identity.PublicKey)
		}
		security.trusted[identity.PublicKey] = identity
	}
	self, ok := security.trusted[publicKey]
	if !ok {
		pair.Wipe()
		return nil, errors.New("当前 NKey 公钥不在传输身份信任文档中")
	}
	security.self = self
	return security, nil
}

func authorizeCapability(identity TransportIdentity, record Announcement) error {
	if !containsIdentityValue(identity.AllowedCapabilities, record.Capability) {
		return fmt.Errorf("身份 %s 未获 capability %s 调用授权", identity.Name, record.Capability)
	}
	switch record.Visibility {
	case "local":
		if identity.NodeID == "" || identity.NodeID != record.NodeID {
			return errors.New("local capability 只允许同一内核调用")
		}
	case "service":
		if (record.ServiceRole == "" && !containsIdentityValue(identity.ServiceRoles, "*")) ||
			(record.ServiceRole != "" && !containsIdentityValue(identity.ServiceRoles, record.ServiceRole)) {
			return fmt.Errorf("身份 %s 不属于 service role %s", identity.Name, record.ServiceRole)
		}
	case "cluster":
		if (record.LogicalService == "" && !containsIdentityValue(identity.LogicalServices, "*")) ||
			(record.LogicalService != "" && !containsIdentityValue(identity.LogicalServices, record.LogicalService)) {
			return fmt.Errorf("身份 %s 不属于 logical service %s", identity.Name, record.LogicalService)
		}
	case "global":
		if !identity.AllowGlobal {
			return fmt.Errorf("身份 %s 未获 global capability 授权", identity.Name)
		}
	default:
		return fmt.Errorf("capability visibility 非法: %q", record.Visibility)
	}
	return nil
}

func containsIdentityValue(values []string, want string) bool {
	for _, value := range values {
		if value == "*" || value == want {
			return true
		}
	}
	return false
}

func (s *TransportSecurity) Close() {
	if s != nil && s.pair != nil {
		s.pair.Wipe()
	}
}

// SelfIdentity 返回当前进程在传输信任文档中的只读身份快照。
func (s *TransportSecurity) SelfIdentity() TransportIdentity {
	if s == nil {
		return TransportIdentity{}
	}
	return s.self
}

// AttestNodeLease binds a Node Lease to the transport identity that will later
// sign cross-node capability traffic. The lease stays non-secret but cannot be
// replaced with a self-reported public key by a KV reader or another node.
func (s *TransportSecurity) AttestNodeLease(record controlplane.NodeRecord) (controlplane.NodeRecord, error) {
	if s == nil || s.pair == nil {
		return controlplane.NodeRecord{}, errors.New("节点租约签名器未配置")
	}
	if err := record.ValidateBasic(); err != nil {
		return controlplane.NodeRecord{}, err
	}
	if !nodeTransportRole(s.self.Role) || s.self.NodeID != record.NodeID || s.self.TenantID != record.TenantID {
		return controlplane.NodeRecord{}, errors.New("节点租约身份与传输信任身份不匹配")
	}
	record.TransportPublicKey = ""
	record.TransportTimestamp = ""
	record.TransportNonce = ""
	record.TransportSignature = ""
	payload, err := json.Marshal(record)
	if err != nil {
		return controlplane.NodeRecord{}, err
	}
	values, err := s.sign(nodeLeaseSubject(record.TenantID, record.Deployment, record.NodeID), payload)
	if err != nil {
		return controlplane.NodeRecord{}, err
	}
	record.TransportPublicKey = values[transportPublicKeyHeader]
	record.TransportTimestamp = values[transportTimestampHeader]
	record.TransportNonce = values[transportNonceHeader]
	record.TransportSignature = values[transportSignatureHeader]
	return record, nil
}

// VerifyNodeLease verifies the detached transport signature without consuming
// replay state: the same current KV revision may be observed by many readers.
func (s *TransportSecurity) VerifyNodeLease(record controlplane.NodeRecord) (TransportIdentity, error) {
	if err := record.ValidateBasic(); err != nil {
		return TransportIdentity{}, err
	}
	values := map[string]string{
		transportPublicKeyHeader: record.TransportPublicKey,
		transportTimestampHeader: record.TransportTimestamp,
		transportNonceHeader:     record.TransportNonce,
		transportSignatureHeader: record.TransportSignature,
	}
	record.TransportPublicKey = ""
	record.TransportTimestamp = ""
	record.TransportNonce = ""
	record.TransportSignature = ""
	payload, err := json.Marshal(record)
	if err != nil {
		return TransportIdentity{}, err
	}
	identity, err := s.verifyNoReplay(nodeLeaseSubject(record.TenantID, record.Deployment, record.NodeID), payload, values)
	if err != nil {
		return TransportIdentity{}, err
	}
	if !nodeTransportRole(identity.Role) || identity.NodeID != record.NodeID || identity.TenantID != record.TenantID || identity.PublicKey != values[transportPublicKeyHeader] {
		return TransportIdentity{}, errors.New("节点租约声明与可信传输身份不匹配")
	}
	return identity, nil
}

func nodeLeaseSubject(tenant, deployment, nodeID string) string {
	return "vastplan.node-lease.v3:" + controlplane.NodeKey(tenant, deployment, nodeID)
}

func nodeTransportRole(role string) bool {
	return role == string(controlplane.RoleNode) || role == string(controlplane.RoleManager)
}

func (s *TransportSecurity) sign(subject string, payload []byte) (map[string]string, error) {
	if s == nil || s.pair == nil {
		return nil, errors.New("传输签名器未配置")
	}
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := randomID()
	signature, err := s.pair.Sign(transportSigningBytes(subject, timestamp, nonce, payload))
	if err != nil {
		return nil, fmt.Errorf("签名传输信封: %w", err)
	}
	return map[string]string{
		transportPublicKeyHeader: s.publicKey,
		transportTimestampHeader: timestamp,
		transportNonceHeader:     nonce,
		transportSignatureHeader: base64.RawURLEncoding.EncodeToString(signature),
	}, nil
}

func (s *TransportSecurity) signMessage(message *nats.Msg) error {
	headers, err := s.sign(message.Subject, message.Data)
	if err != nil {
		return err
	}
	if message.Header == nil {
		message.Header = nats.Header{}
	}
	for key, value := range headers {
		message.Header.Set(key, value)
	}
	return nil
}

func (s *TransportSecurity) verifyMessage(message *nats.Msg) (TransportIdentity, error) {
	return s.verify(message.Subject, message.Data, transportHeaderValues(message.Header))
}

func (s *TransportSecurity) signAnnouncement(key string, record Announcement) (Announcement, error) {
	record.TransportPublicKey = ""
	record.TransportTimestamp = ""
	record.TransportNonce = ""
	record.TransportSignature = ""
	payload, err := announcementPayload(record)
	if err != nil {
		return Announcement{}, err
	}
	headers, err := s.sign(key, payload)
	if err != nil {
		return Announcement{}, err
	}
	record.TransportPublicKey = headers[transportPublicKeyHeader]
	record.TransportTimestamp = headers[transportTimestampHeader]
	record.TransportNonce = headers[transportNonceHeader]
	record.TransportSignature = headers[transportSignatureHeader]
	return record, nil
}

func (s *TransportSecurity) verifyAnnouncement(key string, record Announcement) (TransportIdentity, error) {
	values := map[string]string{
		transportPublicKeyHeader: record.TransportPublicKey,
		transportTimestampHeader: record.TransportTimestamp,
		transportNonceHeader:     record.TransportNonce,
		transportSignatureHeader: record.TransportSignature,
	}
	record.TransportPublicKey = ""
	record.TransportTimestamp = ""
	record.TransportNonce = ""
	record.TransportSignature = ""
	payload, err := announcementPayload(record)
	if err != nil {
		return TransportIdentity{}, err
	}
	return s.verify(key, payload, values)
}

func announcementPayload(record Announcement) ([]byte, error) {
	return json.Marshal(record)
}

func (s *TransportSecurity) verify(subject string, payload []byte, values map[string]string) (TransportIdentity, error) {
	return s.verifyWithReplay(subject, payload, values, true)
}

func (s *TransportSecurity) verifyNoReplay(subject string, payload []byte, values map[string]string) (TransportIdentity, error) {
	return s.verifyWithReplay(subject, payload, values, false)
}

func (s *TransportSecurity) verifyWithReplay(subject string, payload []byte, values map[string]string, checkReplay bool) (TransportIdentity, error) {
	if s == nil {
		return TransportIdentity{}, errors.New("传输验证器未配置")
	}
	publicKey := values[transportPublicKeyHeader]
	identity, trusted := s.trusted[publicKey]
	if !trusted {
		return TransportIdentity{}, errors.New("传输身份不受信任")
	}
	timestamp := values[transportTimestampHeader]
	millis, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return TransportIdentity{}, errors.New("传输签名时间非法")
	}
	when := time.UnixMilli(millis)
	if delta := time.Since(when); delta > s.maxSkew || delta < -s.maxSkew {
		return TransportIdentity{}, errors.New("传输签名超出允许时间窗")
	}
	nonce := values[transportNonceHeader]
	if nonce == "" {
		return TransportIdentity{}, errors.New("传输签名缺少 nonce")
	}
	signature, err := base64.RawURLEncoding.DecodeString(values[transportSignatureHeader])
	if err != nil {
		return TransportIdentity{}, errors.New("传输签名编码非法")
	}
	publicPair, err := nkeys.FromPublicKey(publicKey)
	if err != nil {
		return TransportIdentity{}, errors.New("传输身份公钥非法")
	}
	if err := publicPair.Verify(transportSigningBytes(subject, timestamp, nonce, payload), signature); err != nil {
		return TransportIdentity{}, errors.New("传输签名校验失败")
	}
	if checkReplay {
		if err := s.markNonce(publicKey+":"+nonce, when); err != nil {
			return TransportIdentity{}, err
		}
	}
	return identity, nil
}

func transportHeaderValues(headers nats.Header) map[string]string {
	values := map[string]string{}
	for _, key := range transportHeaderKeys() {
		values[key] = headers.Get(key)
	}
	return values
}

func (s *TransportSecurity) markNonce(key string, when time.Time) error {
	now := time.Now()
	s.replayMu.Lock()
	defer s.replayMu.Unlock()
	for existing, seenAt := range s.replay {
		if now.Sub(seenAt) > s.maxSkew {
			delete(s.replay, existing)
		}
	}
	if _, exists := s.replay[key]; exists {
		return errors.New("检测到传输信封重放")
	}
	s.replay[key] = when
	return nil
}

func transportSigningBytes(subject, timestamp, nonce string, payload []byte) []byte {
	digest := sha256.Sum256(payload)
	return []byte(subject + "\n" + timestamp + "\n" + nonce + "\n" + base64.RawURLEncoding.EncodeToString(digest[:]))
}

func transportHeaderKeys() []string {
	keys := []string{transportPublicKeyHeader, transportTimestampHeader, transportNonceHeader, transportSignatureHeader}
	sort.Strings(keys)
	return keys
}

func authenticatedTransportTrustedContext(identity TransportIdentity, untrusted *contractv1.CallContext) (callcontext.Trusted, error) {
	callCtx := &contractv1.CallContext{}
	if untrusted != nil {
		callCtx = proto.Clone(untrusted).(*contractv1.CallContext)
	}
	if identity.TenantID != "" {
		if callCtx.TenantId != "" && callCtx.TenantId != identity.TenantID {
			return callcontext.Trusted{}, errors.New("调用租户与传输身份租户不一致")
		}
		if principalTenant := callCtx.GetPrincipal().GetTenantId(); principalTenant != "" && principalTenant != identity.TenantID {
			return callcontext.Trusted{}, errors.New("Principal 租户与传输身份租户不一致")
		}
		callCtx.TenantId = identity.TenantID
	}
	// Delegation may preserve an authenticated end-user/plugin/agent identity,
	// but SYSTEM is transport authority and must never be self-asserted on the
	// wire. Rebuild it from the NKey trust document even for delegating edges.
	if !identity.AllowDelegation || callCtx.Caller == nil || callCtx.Caller.Kind == contractv1.CallerKind_CALLER_KIND_SYSTEM || callCtx.Caller.Kind == contractv1.CallerKind_CALLER_KIND_UNSPECIFIED {
		callCtx.Principal = nil
		callCtx.Caller = &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: identity.Name}
	}
	return callcontext.ValidateIngress(callCtx, callcontext.Provenance{
		Source: "addressing.transport", AuthenticatedBy: "nkey-envelope",
		TransportIdentity: identity.Name, TransportRole: identity.Role,
	})
}

func authenticatedTransportContext(identity TransportIdentity, untrusted *contractv1.CallContext) (*contractv1.CallContext, error) {
	trusted, err := authenticatedTransportTrustedContext(identity, untrusted)
	if err != nil {
		return nil, err
	}
	return trusted.Wire(), nil
}
