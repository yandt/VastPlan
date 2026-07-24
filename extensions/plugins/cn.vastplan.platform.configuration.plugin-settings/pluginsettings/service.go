// Package pluginsettings implements the durable generic plugin configuration
// coordinator. It owns candidate intent and audit, never effective Deployment
// state or credential material.
package pluginsettings

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = pluginconfiguration.PluginSettingsID
	PluginVersion = "0.12.1"
	Capability    = "platform.plugin-configuration"
	maxStateBytes = 1 << 20
	maxCandidates = 2048
)

var (
	ErrInvalid  = errors.New("插件配置请求无效")
	ErrNotFound = errors.New("插件配置资源不存在")
	ErrConflict = errors.New("插件配置候选冲突")
)

type AuditEvent struct {
	ID              uint64 `json:"id"`
	CandidateID     string `json:"candidateId"`
	ConfigurationID string `json:"configurationId"`
	Action          string `json:"action"`
	ActorID         string `json:"actorId"`
	At              string `json:"at"`
}

type tenantState struct {
	NextAudit           uint64                                     `json:"nextAudit"`
	Candidates          map[string]pluginconfiguration.Candidate   `json:"candidates"`
	Current             map[string]string                          `json:"current"`
	Audit               []AuditEvent                               `json:"audit"`
	CredentialStages    map[string]map[string]credentialStage      `json:"credentialStages,omitempty"`
	HotDraftBases       map[string]configurationv1.ActiveReference `json:"hotDraftBases,omitempty"`
	HotActivations      map[string]hotActivationRecord             `json:"hotActivations,omitempty"`
	ResourceActivations map[string]resourceActivationRecord        `json:"resourceActivations,omitempty"`
	ScopedActives       map[string]scopedActiveRecord              `json:"scopedActives,omitempty"`
	ScopedDraftBases    map[string]scopedActiveReference           `json:"scopedDraftBases,omitempty"`
	ScopedActivations   map[string]scopedActivationRecord          `json:"scopedActivations,omitempty"`
}

type credentialStage struct {
	FieldID string                        `json:"fieldId"`
	Stage   pluginconfig.StagedCredential `json:"stage"`
	State   string                        `json:"state"`
}

type persistedState struct {
	Tenants map[string]*tenantState `json:"tenants"`
}

type Service struct {
	mu            sync.Mutex
	workflowMu    sync.Mutex
	state         persistedState
	session       *stateSession
	testSave      func(persistedState) error
	now           func() time.Time
	newID         func() (string, error)
	newResourceID func() (string, error)
	scopedChanged map[string]chan struct{}
}

func New() *Service {
	return &Service{
		state:         persistedState{Tenants: map[string]*tenantState{}},
		now:           func() time.Time { return time.Now().UTC() },
		newID:         randomID,
		newResourceID: randomResourceID,
		scopedChanged: map[string]chan struct{}{},
	}
}

func (s *Service) withTenantState(ctx context.Context, host sdk.Host, call *contractv1.CallContext, work func() error) error {
	tenant, _, err := tenantAndActor(call)
	if err != nil {
		return err
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if s.testSave != nil {
		return work()
	}
	if err := s.openStateSession(ctx, host, call, tenant); err != nil {
		return err
	}
	defer s.closeStateSession()
	return work()
}

func tenantAndActor(call *contractv1.CallContext) (string, string, error) {
	if call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return "", "", ErrInvalid
	}
	actor := call.GetPrincipal().GetUserId()
	if actor == "" {
		actor = call.GetCaller().GetId()
	}
	if strings.TrimSpace(actor) == "" {
		return "", "", ErrInvalid
	}
	return call.GetTenantId(), actor, nil
}
