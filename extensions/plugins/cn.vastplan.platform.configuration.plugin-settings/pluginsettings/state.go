package pluginsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

const (
	stateNamespace = "configuration.coordinator"
	stateKey       = "tenant"
)

type stateSession struct {
	ctx        context.Context
	call       *contractv1.CallContext
	repository *tenantStateRepository
	tenant     string
	revision   uint64
}

type tenantStateRepository struct{ client *sharedstatesdk.Client }

func newTenantStateRepository(host sdk.Host) (*tenantStateRepository, error) {
	client, err := sharedstatesdk.New(host, "tenant", stateNamespace)
	if err != nil {
		return nil, err
	}
	return &tenantStateRepository{client: client}, nil
}

func (r *tenantStateRepository) load(ctx context.Context, call *contractv1.CallContext) (*tenantState, uint64, error) {
	entry, err := r.client.Get(ctx, call, stateKey)
	if sharedstatesdk.IsNotFound(err) {
		return emptyTenantState(), 0, nil
	}
	if err != nil {
		return nil, 0, fmt.Errorf("读取插件配置 Shared State: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(entry.Value))
	decoder.DisallowUnknownFields()
	state := emptyTenantState()
	if err := decoder.Decode(state); err != nil {
		return nil, 0, fmt.Errorf("解析插件配置 Shared State: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, 0, errors.New("插件配置 Shared State 包含尾随数据")
	}
	return state, entry.Revision, nil
}

func (r *tenantStateRepository) save(ctx context.Context, call *contractv1.CallContext, state *tenantState, expected uint64) (uint64, error) {
	raw, err := json.Marshal(state)
	if err != nil {
		return 0, err
	}
	if len(raw) > maxStateBytes {
		return 0, errors.New("插件配置租户聚合超过 Shared State 单值上限")
	}
	var entry sharedstatesdk.Entry
	if expected == 0 {
		entry, err = r.client.Create(ctx, call, stateKey, raw)
	} else {
		entry, err = r.client.Update(ctx, call, stateKey, raw, expected)
	}
	if sharedstatesdk.IsConflict(err) {
		return 0, ErrConflict
	}
	if err != nil {
		return 0, fmt.Errorf("保存插件配置 Shared State: %w", err)
	}
	return entry.Revision, nil
}

func (s *Service) openStateSession(ctx context.Context, host sdk.Host, call *contractv1.CallContext, tenant string) error {
	if s.session != nil {
		return errors.New("插件配置状态会话发生嵌套")
	}
	repository, err := newTenantStateRepository(host)
	if err != nil {
		return fmt.Errorf("插件配置 Shared State client 不可用: %w", err)
	}
	state, revision, err := repository.load(ctx, call)
	if err != nil {
		return err
	}
	s.state = persistedState{Tenants: map[string]*tenantState{tenant: state}}
	s.session = &stateSession{ctx: ctx, call: call, repository: repository, tenant: tenant, revision: revision}
	if err := s.validateLoaded(); err != nil {
		s.session = nil
		return err
	}
	return nil
}

func (s *Service) closeStateSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = nil
	s.state = persistedState{Tenants: map[string]*tenantState{}}
}

func (s *Service) validateLoaded() error {
	for tenant, state := range s.state.Tenants {
		if strings.TrimSpace(tenant) == "" || state == nil {
			return errors.New("插件配置协调器状态包含无效租户")
		}
		initializeTenantState(state)
		if len(state.Candidates) > maxCandidates {
			return errors.New("插件配置协调器候选数量超过上限")
		}
		for id, candidate := range state.Candidates {
			validValues := json.Valid(candidate.Values) || (candidate.ApplyPath == pluginconfiguration.ApplyResourceProfile && candidate.ResourceAction == string(configurationresourcev1.ActionDelete) && len(candidate.Values) == 0)
			if id == "" || candidate.ID != id || candidate.ConfigurationID == "" || candidate.Revision == 0 || !pluginconfiguration.ValidCandidateStatus(candidate.Status) || !pluginconfiguration.ValidApplyPath(candidate.ApplyPath) || !validValues {
				return fmt.Errorf("插件配置协调器状态包含无效候选 %q", id)
			}
			for _, status := range candidate.ManagedCredentials {
				if strings.TrimSpace(status.FieldID) == "" || strings.TrimSpace(status.State) == "" || status.Version < 0 ||
					((status.State == "Retained" || status.State == "Staged" || status.State == "Candidate" || status.State == "Active") && status.Version < 1) {
					return fmt.Errorf("插件配置协调器候选 %q 包含无效凭证状态", id)
				}
			}
		}
		for candidateID, fields := range state.CredentialStages {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ID != candidateID {
				return fmt.Errorf("插件配置协调器凭证阶段指向未知候选 %q", candidateID)
			}
			for fieldID, stage := range fields {
				if fieldID == "" || stage.FieldID != fieldID || stage.Stage.ID == "" || stage.Stage.Ref.Handle == "" || (stage.State != "Staged" && stage.State != "Candidate" && stage.State != "Active") {
					return fmt.Errorf("插件配置协调器候选 %q 包含无效凭证阶段", candidateID)
				}
			}
		}
		for configurationID, candidateID := range state.Current {
			candidate, ok := state.Candidates[candidateID]
			if configurationID == "" || !ok || candidateCurrentKey(candidate) != configurationID || terminal(candidate.Status) {
				return fmt.Errorf("插件配置协调器 current 指向无效候选 %q", candidateID)
			}
		}
		for candidateID, activation := range state.ResourceActivations {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ID != candidateID || candidate.ApplyPath != pluginconfiguration.ApplyResourceProfile {
				return fmt.Errorf("插件配置协调器 resource activation 指向未知候选 %q", candidateID)
			}
			if err := activation.validate(candidate, tenant); err != nil {
				return fmt.Errorf("插件配置协调器 resource activation %q 无效: %w", candidateID, err)
			}
		}
		for candidateID, activation := range state.HotActivations {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ID != candidateID || candidate.ApplyPath != pluginconfiguration.ApplyHotService {
				return fmt.Errorf("插件配置协调器 hot activation 指向未知候选 %q", candidateID)
			}
			if err := activation.validate(candidate, tenant); err != nil {
				return fmt.Errorf("插件配置协调器 hot activation %q 无效: %w", candidateID, err)
			}
		}
		for candidateID, base := range state.HotDraftBases {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ApplyPath != pluginconfiguration.ApplyHotService || candidate.Status != pluginconfiguration.CandidateDraft || base.Revision == 0 || len(base.Digest) != 64 {
				return fmt.Errorf("插件配置协调器 hot draft 基线 %q 无效", candidateID)
			}
		}
		for key, active := range state.ScopedActives {
			if err := active.validate(key); err != nil {
				return fmt.Errorf("插件配置协调器 scoped active %q 无效: %w", key, err)
			}
		}
		for candidateID, base := range state.ScopedDraftBases {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ApplyPath != pluginconfiguration.ApplyHotScoped || candidate.Status != pluginconfiguration.CandidateDraft || base.Digest == "" {
				return fmt.Errorf("插件配置协调器 scoped draft 基线 %q 无效", candidateID)
			}
		}
		for candidateID, activation := range state.ScopedActivations {
			candidate, ok := state.Candidates[candidateID]
			if !ok || candidate.ApplyPath != pluginconfiguration.ApplyHotScoped || candidate.Status != pluginconfiguration.CandidatePublishing {
				return fmt.Errorf("插件配置协调器 scoped activation 指向未知候选 %q", candidateID)
			}
			if err := activation.validate(candidate); err != nil {
				return fmt.Errorf("插件配置协调器 scoped activation %q 无效: %w", candidateID, err)
			}
		}
	}
	return nil
}

func (s *Service) saveLocked() error {
	if s.session == nil {
		if s.testSave != nil {
			return s.testSave(s.state)
		}
		return errors.New("插件配置写入缺少 Shared State 会话")
	}
	state := s.state.Tenants[s.session.tenant]
	if state == nil {
		return errors.New("插件配置写入缺少租户聚合")
	}
	if err := s.validateLoaded(); err != nil {
		return err
	}
	revision, err := s.session.repository.save(s.session.ctx, s.session.call, state, s.session.revision)
	if err != nil {
		return err
	}
	s.session.revision = revision
	return nil
}

func emptyTenantState() *tenantState {
	state := &tenantState{}
	initializeTenantState(state)
	return state
}

func initializeTenantState(state *tenantState) {
	if state.Candidates == nil {
		state.Candidates = map[string]pluginconfiguration.Candidate{}
	}
	if state.Current == nil {
		state.Current = map[string]string{}
	}
	if state.CredentialStages == nil {
		state.CredentialStages = map[string]map[string]credentialStage{}
	}
	if state.HotActivations == nil {
		state.HotActivations = map[string]hotActivationRecord{}
	}
	if state.HotDraftBases == nil {
		state.HotDraftBases = map[string]configurationv1.ActiveReference{}
	}
	if state.ResourceActivations == nil {
		state.ResourceActivations = map[string]resourceActivationRecord{}
	}
	if state.ScopedActives == nil {
		state.ScopedActives = map[string]scopedActiveRecord{}
	}
	if state.ScopedDraftBases == nil {
		state.ScopedDraftBases = map[string]scopedActiveReference{}
	}
	if state.ScopedActivations == nil {
		state.ScopedActivations = map[string]scopedActivationRecord{}
	}
}

func (s *Service) tenantLocked(id string) *tenantState {
	state := s.state.Tenants[id]
	if state == nil {
		state = emptyTenantState()
		s.state.Tenants[id] = state
	}
	return state
}
