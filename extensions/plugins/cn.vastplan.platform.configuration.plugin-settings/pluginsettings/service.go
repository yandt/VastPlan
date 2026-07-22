// Package pluginsettings implements the durable generic plugin configuration
// coordinator. It owns candidate intent and audit, never effective Deployment
// state or credential material.
package pluginsettings

import (
	"errors"
	"strings"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
)

const (
	PluginID           = pluginconfiguration.PluginSettingsID
	PluginVersion      = "0.3.0"
	Capability         = "platform.plugin-configuration"
	StateFileConfigKey = "platform.plugin-configuration.stateFile"
	maxStateBytes      = 8 << 20
	maxCandidates      = 2048
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
	NextAudit  uint64                                   `json:"nextAudit"`
	Candidates map[string]pluginconfiguration.Candidate `json:"candidates"`
	Current    map[string]string                        `json:"current"`
	Audit      []AuditEvent                             `json:"audit"`
}

type persistedState struct {
	Tenants map[string]*tenantState `json:"tenants"`
}

type Service struct {
	mu        sync.Mutex
	stateFile string
	state     persistedState
	now       func() time.Time
	newID     func() (string, error)
}

func New(stateFile string) (*Service, error) {
	service := &Service{
		state: persistedState{Tenants: map[string]*tenantState{}},
		now:   func() time.Time { return time.Now().UTC() },
		newID: randomID,
	}
	if strings.TrimSpace(stateFile) != "" {
		if err := service.configure(stateFile); err != nil {
			return nil, err
		}
	}
	return service, nil
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
