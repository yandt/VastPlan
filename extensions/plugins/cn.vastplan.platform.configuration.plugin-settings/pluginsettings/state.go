package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	configurationresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationresource/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) configure(stateFile string) error {
	if !filepath.IsAbs(stateFile) || filepath.Clean(stateFile) != stateFile {
		return errors.New("插件配置协调器 stateFile 必须是规范绝对路径")
	}
	if s.stateFile != "" && s.stateFile != stateFile {
		return errors.New("插件配置协调器 stateFile 不允许运行中切换")
	}
	if s.stateFile != "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(stateFile), 0o700); err != nil {
		return err
	}
	if err := secureDirectory(filepath.Dir(stateFile)); err != nil {
		return err
	}
	s.stateFile = stateFile
	return s.load()
}

func (s *Service) ensureConfigured(ctx context.Context, host sdk.Host, call *contractv1.CallContext) error {
	s.mu.Lock()
	configured := s.stateFile != ""
	s.mu.Unlock()
	if configured {
		return nil
	}
	if host == nil {
		return errors.New("插件配置协调器缺少可信宿主")
	}
	operation := "get"
	payload, _ := json.Marshal(map[string]string{"key": StateFileConfigKey})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("未提供插件配置协调器部署配置")
	}
	var stateFile string
	if err := json.Unmarshal(raw, &stateFile); err != nil {
		return errors.New("插件配置协调器 stateFile 必须是 JSON 字符串")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configure(stateFile)
}

func (s *Service) load() error {
	info, err := os.Lstat(s.stateFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() <= 0 || info.Size() > maxStateBytes {
		return errors.New("插件配置协调器状态文件必须是仅属主可访问且大小受限的普通文件")
	}
	raw, err := os.ReadFile(s.stateFile)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &s.state); err != nil {
		return fmt.Errorf("解析插件配置协调器状态: %w", err)
	}
	if s.state.Tenants == nil {
		s.state.Tenants = map[string]*tenantState{}
	}
	return s.validateLoaded()
}

func (s *Service) validateLoaded() error {
	for tenant, state := range s.state.Tenants {
		if strings.TrimSpace(tenant) == "" || state == nil {
			return errors.New("插件配置协调器状态包含无效租户")
		}
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
	raw, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	if len(raw) > maxStateBytes {
		return errors.New("插件配置协调器状态超过上限")
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.stateFile), ".plugin-settings-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return err
	}
	if _, err := temporary.Write(raw); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.stateFile); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(s.stateFile))
	if err != nil {
		return err
	}
	syncErr, closeErr := directory.Sync(), directory.Close()
	if syncErr != nil {
		return syncErr
	}
	return closeErr
}

func secureDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("插件配置协调器状态目录不能是符号链接或被 group/other 写入")
	}
	return nil
}

func (s *Service) tenantLocked(id string) *tenantState {
	state := s.state.Tenants[id]
	if state == nil {
		state = &tenantState{Candidates: map[string]pluginconfiguration.Candidate{}, Current: map[string]string{}, CredentialStages: map[string]map[string]credentialStage{}, HotDraftBases: map[string]configurationv1.ActiveReference{}, HotActivations: map[string]hotActivationRecord{}, ResourceActivations: map[string]resourceActivationRecord{}, ScopedActives: map[string]scopedActiveRecord{}, ScopedDraftBases: map[string]scopedActiveReference{}, ScopedActivations: map[string]scopedActivationRecord{}}
		s.state.Tenants[id] = state
	}
	return state
}
