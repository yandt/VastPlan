// Package interactionapi defines the neutral control-plane API for durable
// human interactions. It contains neither Portal nor Mobile runtime types.
package interactionapi

import (
	"context"
	"errors"
	"time"

	uiv1 "cdsoft.com.cn/VastPlan/schemas/ui/v1"
)

const Capability = "platform.interaction-broker"

var (
	ErrForbidden    = errors.New("没有处理此交互任务的权限")
	ErrNotFound     = errors.New("交互任务不存在")
	ErrConflict     = errors.New("交互任务状态已变化")
	ErrExpired      = errors.New("交互任务已过期")
	ErrInvalidState = errors.New("交互任务状态不允许此操作")
)

type Subject struct {
	ID       string   `json:"id"`
	TenantID string   `json:"tenantId"`
	Roles    []string `json:"roles,omitempty"`
	System   bool     `json:"system,omitempty"`
}

type State string

const (
	StateCreated   State = "created"
	StatePresented State = "presented"
	StateAnswered  State = "answered"
	StateRejected  State = "rejected"
	StateCancelled State = "cancelled"
	StateExpired   State = "expired"
)

func (s State) Terminal() bool {
	return s == StateAnswered || s == StateRejected || s == StateCancelled || s == StateExpired
}

type AuditEvent struct {
	Action  string    `json:"action"`
	ActorID string    `json:"actorId"`
	Surface string    `json:"surface,omitempty"`
	At      time.Time `json:"at"`
}

// Record intentionally keeps audit metadata separate from Response, so audit
// streams never serialize form values or credential references.
type Record struct {
	Request     uiv1.InteractionRequest   `json:"request"`
	State       State                     `json:"state"`
	Response    *uiv1.InteractionResponse `json:"response,omitempty"`
	CreatedAt   time.Time                 `json:"createdAt"`
	UpdatedAt   time.Time                 `json:"updatedAt"`
	PresentedBy string                    `json:"presentedBy,omitempty"`
	Audit       []AuditEvent              `json:"audit"`
}

type Service interface {
	Open(context.Context, Subject, uiv1.InteractionRequest) (Record, error)
	List(context.Context, Subject, uiv1.InteractionSurface) ([]Record, error)
	Get(context.Context, Subject, string) (Record, error)
	Present(context.Context, Subject, string, uiv1.InteractionSurface) (Record, error)
	Respond(context.Context, Subject, string, uiv1.InteractionSurface, uiv1.InteractionResponse) (Record, error)
	Cancel(context.Context, Subject, string) (Record, error)
}
