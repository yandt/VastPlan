package authorizationv1

import (
	"encoding/json"
	"fmt"
)

const (
	ProviderSchemaURL = "https://schemas.cdsoft.com.cn/vastplan/authorization/v1/vastplan.authorization-provider.schema.json"

	ProtocolStore     = "authorization.store.v1"
	ProtocolEngine    = "authorization.engine.v1"
	ProtocolDirectory = "authorization.directory.v1"
	ProtocolExchange  = "authorization.exchange.v1"
)

var protocolOperations = map[string][]string{
	ProtocolStore:     {"probe", "load", "compareAndSwap", "watch", "appendAudit", "backup"},
	ProtocolEngine:    {"prepare", "evaluate", "explain", "health"},
	ProtocolDirectory: {"resolveSubject", "resolveGroups", "watchRevision"},
	ProtocolExchange:  {"planImport", "validate", "import", "export"},
}

type ConfigurationRevisionRef struct {
	ProfileID string `json:"profileId"`
	Revision  uint64 `json:"revision"`
	Digest    string `json:"digest"`
}

// ProviderRef selects a protocol implementation without embedding secrets or
// transport addresses. Capability routing and plugin configuration remain host
// controlled.
type ProviderRef struct {
	Protocol      string                   `json:"protocol"`
	ProviderID    string                   `json:"providerId"`
	PluginID      string                   `json:"pluginId"`
	Capability    string                   `json:"capability"`
	Version       string                   `json:"version"`
	Configuration ConfigurationRevisionRef `json:"configuration"`
}

type ProviderProfile struct {
	ID        string        `json:"id"`
	Revision  uint64        `json:"revision"`
	Store     ProviderRef   `json:"store"`
	Engine    ProviderRef   `json:"engine"`
	Directory *ProviderRef  `json:"directory,omitempty"`
	Exchange  []ProviderRef `json:"exchange"`
}

type ProviderDescriptor struct {
	ProviderID          string            `json:"providerId"`
	PluginID            string            `json:"pluginId"`
	Version             string            `json:"version"`
	Protocols           []ProtocolSupport `json:"protocols"`
	ConfigurationSchema json.RawMessage   `json:"configurationSchema"`
}

type ProtocolSupport struct {
	Protocol   string   `json:"protocol"`
	Operations []string `json:"operations"`
}

func ProtocolOperations(protocol string) []string {
	return append([]string(nil), protocolOperations[protocol]...)
}

func KnownProtocolOperation(protocol, operation string) bool {
	for _, candidate := range protocolOperations[protocol] {
		if candidate == operation {
			return true
		}
	}
	return false
}

// NegotiateProtocol selects an exact wire protocol. Major-version fallback is
// forbidden: adapters must explicitly offer the requested protocol and its
// complete operation set.
func NegotiateProtocol(required string, offered []ProtocolSupport) (ProtocolSupport, error) {
	for _, support := range offered {
		if support.Protocol != required {
			continue
		}
		if err := validateProtocolSupport(support); err != nil {
			return ProtocolSupport{}, err
		}
		return support, nil
	}
	return ProtocolSupport{}, fmt.Errorf("Provider 未提供所需协议 %s", required)
}
