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
	"path/filepath"
	"sort"
	"strings"
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
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
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
