// Package artifactrepositoryv1 defines the two repository protocols that may
// back normal plugin delivery. Bootstrap Seed is intentionally not a third
// repository protocol.
package artifactrepositoryv1

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	semver "github.com/Masterminds/semver/v3"
)

const (
	ProfileVersion = 1

	ProtocolLocalTest = "artifact.repository.local-test.v1"
	ProtocolRemote    = "artifact.repository.remote.v1"
)

const (
	OperationReadExact       = "readExact"
	OperationPublish         = "publish"
	OperationCatalogSnapshot = "catalogSnapshot"
	OperationExpireWorkspace = "expireWorkspace"
	OperationJournal         = "journal"
	OperationResolveLock     = "resolveLock"
	OperationPromote         = "promote"
	OperationRetire          = "retire"
	OperationEvidence        = "evidence"
	OperationReferences      = "references"
	OperationGarbageCollect  = "garbageCollect"
)

var protocolOperations = map[string][]string{
	ProtocolLocalTest: {
		OperationReadExact, OperationPublish, OperationCatalogSnapshot, OperationExpireWorkspace,
	},
	ProtocolRemote: {
		OperationReadExact, OperationPublish, OperationCatalogSnapshot, OperationJournal,
		OperationResolveLock, OperationPromote, OperationRetire, OperationEvidence,
		OperationReferences, OperationGarbageCollect,
	},
}

// Profile selects one exact repository protocol. Credentials and storage
// provider material remain outside this document and are injected by the
// trusted host.
type Profile struct {
	Version         int              `json:"version"`
	ID              string           `json:"id"`
	Protocol        string           `json:"protocol"`
	Endpoint        string           `json:"endpoint"`
	Channels        []string         `json:"channels"`
	DevelopmentOnly bool             `json:"developmentOnly"`
	Workspace       *WorkspacePolicy `json:"workspace,omitempty"`
}

type WorkspacePolicy struct {
	TTLSeconds   int64 `json:"ttlSeconds"`
	MaxArtifacts int   `json:"maxArtifacts"`
}

// Receipt binds a repository operation to the exact protocol/Profile that
// produced it. A release controller must reject receipts from another binding.
type Receipt struct {
	SchemaVersion  int                  `json:"schemaVersion"`
	RepositoryID   string               `json:"repositoryId"`
	Protocol       string               `json:"protocol"`
	ProfileDigest  string               `json:"profileDigest"`
	Ref            pluginv1.ArtifactRef `json:"ref"`
	SHA256         string               `json:"sha256"`
	Revision       uint64               `json:"revision"`
	WorkspaceLease string               `json:"workspaceLease,omitempty"`
	ExpiresAt      *time.Time           `json:"expiresAt,omitempty"`
}

type CatalogSnapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	RepositoryID  string    `json:"repositoryId"`
	Protocol      string    `json:"protocol"`
	ProfileDigest string    `json:"profileDigest"`
	Revision      uint64    `json:"revision"`
	Items         []Receipt `json:"items"`
}

type ExpireWorkspaceResult struct {
	SchemaVersion int    `json:"schemaVersion"`
	Revision      uint64 `json:"revision"`
	Expired       int    `json:"expired"`
}

func ParseProfile(raw []byte) (Profile, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var profile Profile
	if err := decoder.Decode(&profile); err != nil {
		return Profile{}, fmt.Errorf("解析制品仓库 Profile: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Profile{}, errors.New("制品仓库 Profile 只能包含一个 JSON 文档")
	}
	return ValidateProfile(profile)
}

func ParseProfileFile(filename string) (Profile, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return Profile{}, fmt.Errorf("读取制品仓库 Profile: %w", err)
	}
	return ParseProfile(raw)
}

func ValidateProfile(profile Profile) (Profile, error) {
	if profile.Version != ProfileVersion || !validResourceID(profile.ID) {
		return Profile{}, errors.New("制品仓库 Profile version 或 id 无效")
	}
	if _, known := protocolOperations[profile.Protocol]; !known {
		return Profile{}, fmt.Errorf("未知制品仓库协议 %q", profile.Protocol)
	}
	channels, err := validateChannels(profile.Protocol, profile.Channels)
	if err != nil {
		return Profile{}, err
	}
	profile.Channels = channels
	parsed, err := url.Parse(profile.Endpoint)
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" || parsed.Opaque != "" {
		return Profile{}, errors.New("制品仓库 endpoint 无效")
	}
	switch profile.Protocol {
	case ProtocolLocalTest:
		if !profile.DevelopmentOnly || parsed.Scheme != "unix" || parsed.Host != "" || !filepath.IsAbs(parsed.Path) || filepath.Clean(parsed.Path) != parsed.Path {
			return Profile{}, errors.New("local-test 协议只允许 developmentOnly 的规范绝对 Unix Socket")
		}
		if contains(channels, "workspace") {
			if profile.Workspace == nil || profile.Workspace.TTLSeconds < 60 || profile.Workspace.TTLSeconds > 86400 || profile.Workspace.MaxArtifacts < 1 || profile.Workspace.MaxArtifacts > 10000 {
				return Profile{}, errors.New("local-test workspace 策略必须设置有界 TTL 与容量")
			}
		} else if profile.Workspace != nil {
			return Profile{}, errors.New("未启用 workspace channel 时不得配置 workspace 策略")
		}
	case ProtocolRemote:
		if profile.DevelopmentOnly || parsed.Scheme != "https" || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" || profile.Workspace != nil {
			return Profile{}, errors.New("remote 协议必须使用无内嵌凭证的 HTTPS endpoint，且不得配置 workspace")
		}
	}
	return profile, nil
}

func ProtocolOperations(protocol string) []string {
	return append([]string(nil), protocolOperations[protocol]...)
}

func Supports(protocol, operation string) bool {
	for _, candidate := range protocolOperations[protocol] {
		if candidate == operation {
			return true
		}
	}
	return false
}

func (profile Profile) Digest() string {
	normalized, err := ValidateProfile(profile)
	if err != nil {
		return ""
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

// ValidateReceipt prevents a transport or Provider from returning a receipt
// that belongs to another repository binding. It validates identity and
// lifecycle fields, but does not replace host-side artifact trust checks.
func ValidateReceipt(profile Profile, receipt Receipt) error {
	profile, err := ValidateProfile(profile)
	if err != nil {
		return err
	}
	if err := ValidateReceiptShape(receipt); err != nil {
		return err
	}
	if receipt.SchemaVersion != ProfileVersion || receipt.RepositoryID != profile.ID || receipt.Protocol != profile.Protocol || receipt.ProfileDigest != profile.Digest() {
		return errors.New("制品仓库回执与 Profile 身份不匹配")
	}
	if err := ValidateRef(profile, receipt.Ref); err != nil || !validSHA256(receipt.SHA256) || receipt.Revision == 0 {
		return errors.New("制品仓库回执引用、摘要或 revision 无效")
	}
	if receipt.Ref.Channel == "workspace" {
		if profile.Protocol != ProtocolLocalTest || receipt.WorkspaceLease == "" || len(receipt.WorkspaceLease) > 256 || receipt.ExpiresAt == nil || receipt.ExpiresAt.IsZero() {
			return errors.New("workspace 回执缺少 lease 或过期时间")
		}
	} else if receipt.WorkspaceLease != "" || receipt.ExpiresAt != nil {
		return errors.New("非 workspace 回执不得携带 workspace lease")
	}
	return nil
}

// ValidateReceiptShape is the transport-neutral admission check used before a
// trusted host resolves the active Profile. It does not establish repository
// identity; ValidateReceipt must still run at the repository boundary.
func ValidateReceiptShape(receipt Receipt) error {
	if receipt.SchemaVersion != ProfileVersion || !validResourceID(receipt.RepositoryID) || len(protocolOperations[receipt.Protocol]) == 0 || !validSHA256(receipt.ProfileDigest) || !validPluginID(receipt.Ref.PluginID) || !validSemver(receipt.Ref.Version) || !validSHA256(receipt.SHA256) || receipt.Revision == 0 {
		return errors.New("制品仓库回执基础字段无效")
	}
	allowedChannel := receipt.Ref.Channel == "testing" || receipt.Protocol == ProtocolRemote && (receipt.Ref.Channel == "candidate" || receipt.Ref.Channel == "stable") || receipt.Protocol == ProtocolLocalTest && receipt.Ref.Channel == "workspace"
	if !allowedChannel {
		return errors.New("制品仓库回执 channel 与协议不匹配")
	}
	if receipt.Ref.Channel == "workspace" {
		if receipt.WorkspaceLease == "" || len(receipt.WorkspaceLease) > 256 || receipt.ExpiresAt == nil || receipt.ExpiresAt.IsZero() {
			return errors.New("workspace 回执缺少 lease 或过期时间")
		}
	} else if receipt.WorkspaceLease != "" || receipt.ExpiresAt != nil {
		return errors.New("非 workspace 回执不得携带 workspace lease")
	}
	return nil
}

func ValidateRef(profile Profile, ref pluginv1.ArtifactRef) error {
	profile, err := ValidateProfile(profile)
	if err != nil {
		return err
	}
	if !validPluginID(ref.PluginID) || !validSemver(ref.Version) || !contains(profile.Channels, ref.Channel) {
		return errors.New("精确制品引用不属于 Repository Profile")
	}
	return nil
}

func ValidateCatalogSnapshot(profile Profile, snapshot CatalogSnapshot) error {
	profile, err := ValidateProfile(profile)
	if err != nil {
		return err
	}
	if snapshot.SchemaVersion != ProfileVersion || snapshot.RepositoryID != profile.ID || snapshot.Protocol != profile.Protocol || snapshot.ProfileDigest != profile.Digest() {
		return errors.New("Catalog 快照与 Profile 身份不匹配")
	}
	seen := map[pluginv1.ArtifactRef]bool{}
	for index, receipt := range snapshot.Items {
		if err := ValidateReceipt(profile, receipt); err != nil {
			return fmt.Errorf("Catalog items[%d]: %w", index, err)
		}
		if seen[receipt.Ref] {
			return fmt.Errorf("Catalog items[%d] 包含重复精确引用", index)
		}
		seen[receipt.Ref] = true
		if receipt.Revision > snapshot.Revision {
			return fmt.Errorf("Catalog items[%d] revision 超过快照", index)
		}
	}
	return nil
}

func ValidateExpireWorkspaceResult(profile Profile, result ExpireWorkspaceResult) error {
	profile, err := ValidateProfile(profile)
	if err != nil {
		return err
	}
	if profile.Protocol != ProtocolLocalTest || profile.Workspace == nil || result.SchemaVersion != ProfileVersion || result.Expired < 0 {
		return errors.New("workspace 过期结果与 local-test Profile 不匹配")
	}
	return nil
}

func validateChannels(protocol string, input []string) ([]string, error) {
	if len(input) == 0 || len(input) > 8 {
		return nil, errors.New("制品仓库 channels 数量无效")
	}
	allowed := map[string]bool{"testing": true, "candidate": protocol == ProtocolRemote, "stable": protocol == ProtocolRemote, "workspace": protocol == ProtocolLocalTest}
	channels := append([]string(nil), input...)
	if !sort.StringsAreSorted(channels) {
		return nil, errors.New("制品仓库 channels 必须按字典序排列")
	}
	seen := map[string]bool{}
	for _, channel := range channels {
		if !allowed[channel] || seen[channel] {
			return nil, fmt.Errorf("协议 %s 不允许或重复 channel %q", protocol, channel)
		}
		seen[channel] = true
	}
	if !seen["testing"] {
		return nil, errors.New("两种仓库协议都必须提供 testing channel")
	}
	return channels, nil
}

func validResourceID(value string) bool {
	if value == "" || len(value) > 128 || value != strings.ToLower(value) {
		return false
	}
	for index, char := range value {
		if char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || index > 0 && (char == '.' || char == '_' || char == '-') {
			continue
		}
		return false
	}
	return true
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validPluginID(value string) bool {
	if value == "" || len(value) > 160 {
		return false
	}
	separators := 0
	previousSeparator := true
	for _, char := range value {
		if char == '.' || char == '-' {
			if previousSeparator {
				return false
			}
			separators++
			previousSeparator = true
			continue
		}
		if char < 'a' || char > 'z' && (char < '0' || char > '9') {
			return false
		}
		previousSeparator = false
	}
	return separators > 0 && !previousSeparator
}

func validSemver(value string) bool {
	_, err := semver.StrictNewVersion(value)
	return err == nil
}
