// Package platformadminapi defines the browser-facing platform administration
// contract. It intentionally contains domain DTOs only: transport, plugin IDs,
// NATS subjects and repository credentials stay behind the trusted Portal host.
package platformadminapi

import (
	"encoding/json"
	"errors"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
)

const (
	SettingsCapability            = "platform.settings"
	CredentialsCapability         = "platform.credentials"
	DatabaseCapability            = "platform.database"
	ArtifactsCapability           = "platform.artifacts.repository"
	DeploymentCapability          = "platform.deployment"
	PluginConfigurationCapability = "platform.plugin-configuration"
)

var (
	ErrInvalid  = errors.New("平台管理请求无效")
	ErrNotFound = errors.New("平台管理资源不存在")
	ErrConflict = errors.New("平台管理资源版本冲突")
)

type Setting struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	Version   int64           `json:"version"`
	UpdatedAt string          `json:"updatedAt"`
}

type PutSettingRequest struct {
	Value     json.RawMessage `json:"value"`
	IfVersion *int64          `json:"ifVersion,omitempty"`
}

type CredentialMetadata struct {
	Name       string `json:"name"`
	Version    int64  `json:"version"`
	KeyVersion string `json:"keyVersion"`
	CreatedAt  string `json:"createdAt"`
	UpdatedAt  string `json:"updatedAt"`
	Revoked    bool   `json:"revoked"`
}

// PutCredentialRequest is deliberately write-only. No response type and no
// read operation in this package can carry plaintext or ciphertext.
type PutCredentialRequest struct {
	Value string `json:"value"`
}

type DatabaseConnection struct {
	Name       string                   `json:"name"`
	ResourceID string                   `json:"resourceId"`
	Revision   uint64                   `json:"revision"`
	ProviderID string                   `json:"providerId"`
	Endpoint   string                   `json:"endpoint"`
	Database   string                   `json:"database,omitempty"`
	Options    json.RawMessage          `json:"options"`
	Pool       databasev1.PoolPolicy    `json:"pool"`
	Runtime    string                   `json:"runtime"`
	Credential DatabaseCredentialStatus `json:"credential"`
}

type DatabaseCredentialStatus struct {
	Managed bool  `json:"managed"`
	Version int64 `json:"version"`
}

// PutDatabaseConnectionRequest accepts credential material only as a
// write-only input to the database plugin. The value is omitted on ordinary
// edits to retain the currently managed credential and is never returned.
type PutDatabaseConnectionRequest struct {
	ProviderID      string                 `json:"providerId"`
	Endpoint        string                 `json:"endpoint"`
	Database        string                 `json:"database,omitempty"`
	Options         json.RawMessage        `json:"options"`
	Pool            *databasev1.PoolPolicy `json:"pool,omitempty"`
	CredentialValue string                 `json:"credentialValue,omitempty"`
}

type DatabaseProbe struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}
