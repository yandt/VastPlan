// Package configurationauthority defines the one-use trusted-host authority
// that permits the generic configuration coordinator to stage one managed
// credential for one exact signed configuration field.
package configurationauthority

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

const (
	SchemaVersion        = "v1"
	KernelIssueService   = "kernel.configuration.authority.issue"
	KernelConsumeService = "kernel.configuration.authority.consume"
	CoordinatorPluginID  = "cn.vastplan.platform.configuration.plugin-settings"
	CustodianPluginID    = "cn.vastplan.platform.security.credentials"
	TokenPrefix          = "cauth_"
	DefaultTTL           = 45 * time.Second
	MaximumTTL           = 60 * time.Second
)

var (
	ErrInvalid         = errors.New("配置授权请求无效")
	ErrNotFound        = errors.New("配置授权不存在")
	ErrExpired         = errors.New("配置授权已过期")
	ErrAlreadyConsumed = errors.New("配置授权已被消费")
)

type IssueRequest struct {
	ConfigurationID      string `json:"configurationId"`
	ResourceCollectionID string `json:"resourceCollectionId,omitempty"`
	ResourceID           string `json:"resourceId,omitempty"`
	CatalogDigest        string `json:"catalogDigest"`
	CandidateID          string `json:"candidateId"`
	FieldID              string `json:"fieldId"`
}

type Issued struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// Claims are derived by the trusted host from the active catalog. The browser,
// coordinator and credential custodian cannot override owner or purpose.
type Claims struct {
	SchemaVersion        string    `json:"schemaVersion"`
	AuthorityID          string    `json:"authorityId"`
	TenantID             string    `json:"tenantId"`
	ConfigurationID      string    `json:"configurationId"`
	ResourceCollectionID string    `json:"resourceCollectionId,omitempty"`
	ResourceID           string    `json:"resourceId,omitempty"`
	CatalogDigest        string    `json:"catalogDigest"`
	Deployment           string    `json:"deployment"`
	UnitID               string    `json:"unitId"`
	CandidateID          string    `json:"candidateId"`
	FieldID              string    `json:"fieldId"`
	Owner                string    `json:"owner"`
	Purpose              string    `json:"purpose"`
	Resource             string    `json:"resource"`
	ArtifactSHA256       string    `json:"artifactSha256"`
	SchemaDigest         string    `json:"schemaDigest"`
	IssuedAt             time.Time `json:"issuedAt"`
	ExpiresAt            time.Time `json:"expiresAt"`
}

func (c Claims) Validate(now time.Time, tenant string) error {
	if c.SchemaVersion != SchemaVersion || !validHexID(c.AuthorityID, TokenPrefix, 64) ||
		strings.TrimSpace(c.TenantID) == "" || c.TenantID != tenant ||
		!validHexID(c.ConfigurationID, "cfg_", 24) || !validHexID(c.CandidateID, "pcfg_", 32) ||
		strings.TrimSpace(c.CatalogDigest) == "" || len(c.CatalogDigest) != 64 ||
		strings.TrimSpace(c.Deployment) == "" || strings.TrimSpace(c.UnitID) == "" ||
		strings.TrimSpace(c.FieldID) == "" || strings.TrimSpace(c.Owner) == "" ||
		strings.TrimSpace(c.Purpose) == "" || strings.TrimSpace(c.Resource) == "" ||
		len(c.ArtifactSHA256) != 64 || len(c.SchemaDigest) != 64 || c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() {
		return ErrInvalid
	}
	if (c.ResourceCollectionID == "") != (c.ResourceID == "") {
		return ErrInvalid
	}
	if c.ResourceCollectionID != "" && (!validHexID(c.ResourceCollectionID, "cfgc_", 24) || !validHexID(c.ResourceID, "cfgp_", 32)) {
		return ErrInvalid
	}
	if !c.ExpiresAt.After(c.IssuedAt) || c.ExpiresAt.Sub(c.IssuedAt) > MaximumTTL {
		return ErrInvalid
	}
	if !now.Before(c.ExpiresAt) {
		return ErrExpired
	}
	return nil
}

func validHexID(value, prefix string, hexLength int) bool {
	if len(value) != len(prefix)+hexLength || !strings.HasPrefix(value, prefix) {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil
}

type Issuer interface {
	Issue(context.Context, string, IssueRequest) (Issued, error)
}

type Consumer interface {
	Consume(context.Context, string, string) (Claims, error)
}
