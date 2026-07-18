// Package platformadminapi defines the browser-facing platform administration
// contract. It intentionally contains domain DTOs only: transport, plugin IDs,
// NATS subjects and repository credentials stay behind the Portal Edge.
package platformadminapi

import (
	"context"
	"encoding/json"
	"errors"

	"cdsoft.com.cn/VastPlan/core/shared/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	SettingsCapability    = "platform.settings"
	CredentialsCapability = "platform.credentials"
	DatabaseCapability    = "platform.database"
	ArtifactsCapability   = "platform.artifacts.repository"
	DeploymentCapability  = "platform.deployment"
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
	Name       string `json:"name"`
	Driver     string `json:"driver"`
	Endpoint   string `json:"endpoint"`
	Database   string `json:"database,omitempty"`
	Credential string `json:"credential"`
}

type DatabaseProbe struct {
	Ready   bool   `json:"ready"`
	Message string `json:"message,omitempty"`
}

type ArtifactRepositoryStatus struct {
	Ready  bool   `json:"ready"`
	Listen string `json:"listen,omitempty"`
}

type ManagedNode struct {
	ID        string             `json:"id"`
	Plan      nodebootstrap.Plan `json:"plan"`
	Version   int64              `json:"version"`
	CreatedAt string             `json:"createdAt"`
	UpdatedAt string             `json:"updatedAt"`
}

type PutManagedNodeRequest struct {
	Plan      nodebootstrap.Plan `json:"plan"`
	IfVersion *int64             `json:"ifVersion,omitempty"`
}

type BootstrapJobState string

const (
	BootstrapPending       BootstrapJobState = "Pending"
	BootstrapApproved      BootstrapJobState = "Approved"
	BootstrapConnecting    BootstrapJobState = "Connecting"
	BootstrapInstalling    BootstrapJobState = "Installing"
	BootstrapSystemdActive BootstrapJobState = "SystemdActive"
	BootstrapReady         BootstrapJobState = "Ready"
	BootstrapFailed        BootstrapJobState = "Failed"
	BootstrapExpired       BootstrapJobState = "Expired"
)

type BootstrapJob struct {
	ID          string            `json:"id"`
	NodeID      string            `json:"nodeId"`
	NodeVersion int64             `json:"nodeVersion"`
	State       BootstrapJobState `json:"state"`
	RequestedBy string            `json:"requestedBy"`
	ApprovedBy  string            `json:"approvedBy,omitempty"`
	ErrorCode   string            `json:"errorCode,omitempty"`
	CreatedAt   string            `json:"createdAt"`
	UpdatedAt   string            `json:"updatedAt"`
	ExpiresAt   string            `json:"expiresAt"`
}

// Service is the narrow BFF port consumed by HTTP handlers. Implementations
// may reach local or cluster capabilities, but callers cannot select a target.
type Service interface {
	ListSettings(context.Context, portalapi.Principal, string) ([]Setting, error)
	PutSetting(context.Context, portalapi.Principal, string, PutSettingRequest) (Setting, error)
	DeleteSetting(context.Context, portalapi.Principal, string, *int64) error
	ListCredentials(context.Context, portalapi.Principal, string) ([]CredentialMetadata, error)
	PutCredential(context.Context, portalapi.Principal, string, PutCredentialRequest) (CredentialMetadata, error)
	RotateCredential(context.Context, portalapi.Principal, string) (CredentialMetadata, error)
	RevokeCredential(context.Context, portalapi.Principal, string) (CredentialMetadata, error)
	ListDatabaseConnections(context.Context, portalapi.Principal) ([]DatabaseConnection, error)
	PutDatabaseConnection(context.Context, portalapi.Principal, string, DatabaseConnection) (DatabaseConnection, error)
	DeleteDatabaseConnection(context.Context, portalapi.Principal, string) error
	ProbeDatabaseConnection(context.Context, portalapi.Principal, string) (DatabaseProbe, error)
	ArtifactRepositoryStatus(context.Context, portalapi.Principal) (ArtifactRepositoryStatus, error)
	ListManagedNodes(context.Context, portalapi.Principal) ([]ManagedNode, error)
	PutManagedNode(context.Context, portalapi.Principal, string, PutManagedNodeRequest) (ManagedNode, error)
	ListBootstrapJobs(context.Context, portalapi.Principal) ([]BootstrapJob, error)
	CreateBootstrapJob(context.Context, portalapi.Principal, string) (BootstrapJob, error)
	ApproveBootstrapJob(context.Context, portalapi.Principal, string) (BootstrapJob, error)
}
