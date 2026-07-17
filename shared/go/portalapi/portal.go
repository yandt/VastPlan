// Package portalapi defines the stable control-plane contract between Edge/BFF and
// the portal-composer plugin. It deliberately contains no HTTP or UI framework code.
package portalapi

import (
	"context"
	"errors"
)

var (
	ErrForbidden       = errors.New("没有执行此 Portal 操作的权限")
	ErrNotFound        = errors.New("Portal revision 不存在")
	ErrInvalidState    = errors.New("Portal revision 状态不允许此操作")
	ErrSelfApproval    = errors.New("提交人不能审批自己的草稿")
	ErrRouteConflict   = errors.New("Portal 路由或域名与已发布 Portal 冲突")
	ErrCatalogRejected = errors.New("Portal 制品目录校验失败")
)

type Principal struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenantId"`
	Roles    []string `json:"roles"`
	System   bool     `json:"system,omitempty"`
}

type PluginRef struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Channel string `json:"channel,omitempty"`
}

type DesignSystem struct {
	PluginRef
	UIContract string `json:"uiContract"`
}

type PortalSpec struct {
	ID           string         `json:"id"`
	Route        string         `json:"route"`
	Domains      []string       `json:"domains,omitempty"`
	Audience     []string       `json:"audience,omitempty"`
	Branding     map[string]any `json:"branding,omitempty"`
	DesignSystem DesignSystem   `json:"designSystem"`
	Plugins      []PluginRef    `json:"plugins"`
	Config       map[string]any `json:"config,omitempty"`
}

type Status string

const (
	StatusDraft           Status = "Draft"
	StatusPendingApproval Status = "PendingApproval"
	StatusApproved        Status = "Approved"
	StatusPublished       Status = "Published"
)

type Revision struct {
	ID          uint64     `json:"id"`
	TenantID    string     `json:"tenantId"`
	PortalID    string     `json:"portalId"`
	Status      Status     `json:"status"`
	Active      bool       `json:"active"`
	Spec        PortalSpec `json:"spec"`
	SubmittedBy string     `json:"submittedBy,omitempty"`
	ApprovedBy  string     `json:"approvedBy,omitempty"`
	PublishedBy string     `json:"publishedBy,omitempty"`
	CreatedAt   string     `json:"createdAt"`
	UpdatedAt   string     `json:"updatedAt"`
}

type AuditEvent struct {
	ID         uint64 `json:"id"`
	TenantID   string `json:"tenantId"`
	PortalID   string `json:"portalId"`
	RevisionID uint64 `json:"revisionId"`
	Action     string `json:"action"`
	ActorID    string `json:"actorId"`
	Reason     string `json:"reason,omitempty"`
	Priority   string `json:"priority"`
	At         string `json:"at"`
}

type PublishRequest struct {
	BreakGlassReason string `json:"breakGlassReason,omitempty"`
}

// Service is implemented by the configuration/composition plugin and consumed
// through an authenticated Edge adapter. Every method scopes itself to principal.TenantID.
type Service interface {
	CreateDraft(context.Context, Principal, PortalSpec) (Revision, error)
	List(context.Context, Principal) ([]Revision, error)
	Submit(context.Context, Principal, uint64) (Revision, error)
	Approve(context.Context, Principal, uint64) (Revision, error)
	Publish(context.Context, Principal, uint64, PublishRequest) (Revision, error)
	Rollback(context.Context, Principal, uint64, PublishRequest) (Revision, error)
	Audit(context.Context, Principal, uint64) ([]AuditEvent, error)
}
