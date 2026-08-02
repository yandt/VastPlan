package deploymentmanager

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (s *Service) validateLoaded() error {
	for tenant, state := range s.data.Tenants {
		if strings.TrimSpace(tenant) == "" || state == nil {
			return errors.New("deployment-manager 状态包含无效租户")
		}
		if state.Nodes == nil {
			state.Nodes = map[string]platformadminapi.ManagedNode{}
		}
		if state.Jobs == nil {
			state.Jobs = map[string]platformadminapi.BootstrapJob{}
		}
		if state.TestBindings == nil {
			state.TestBindings = map[string]platformadminapi.TestTargetBinding{}
		}
		if state.InstallationCandidates == nil {
			state.InstallationCandidates = map[string]installationCandidateRecord{}
		}
		if state.ConfigurationRequests == nil {
			state.ConfigurationRequests = map[string]string{}
		}
		if state.ProfileActivations == nil {
			state.ProfileActivations = map[string]profileActivationRecord{}
		}
		for id, activation := range state.ProfileActivations {
			if id != activation.CandidateID || activation.validate(tenant) != nil {
				return fmt.Errorf("deployment-manager 状态包含无效 Platform Profile 激活 %q", id)
			}
		}
		for _, revision := range state.Revisions {
			if validateServiceRevisionRecord(tenant, revision) != nil {
				return fmt.Errorf("deployment-manager 状态包含无效服务组合 revision %d", revision.ID)
			}
			if revision.ConfigurationCandidateID != "" && !validConfigurationRequestHash(state.ConfigurationRequests[revision.ConfigurationCandidateID]) {
				return fmt.Errorf("deployment-manager 配置修订 %d 缺少幂等请求摘要", revision.ID)
			}
		}
		for id, node := range state.Nodes {
			if id == "" || node.ID != id || node.Version < 1 || node.Plan.Node.ID != id || node.Plan.Node.Tenant != tenant || node.Plan.Validate() != nil {
				return fmt.Errorf("deployment-manager 状态包含无效节点 %q", id)
			}
		}
		for id, job := range state.Jobs {
			if id == "" || job.ID != id || job.NodeID == "" || job.NodeVersion < 1 || job.RequestedBy == "" || !validJobState(job.State) {
				return fmt.Errorf("deployment-manager 状态包含无效引导作业 %q", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.CreatedAt); err != nil {
				return fmt.Errorf("引导作业 %q 的创建时间无效", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.UpdatedAt); err != nil {
				return fmt.Errorf("引导作业 %q 的更新时间无效", id)
			}
			if _, err := time.Parse(time.RFC3339Nano, job.ExpiresAt); err != nil {
				return fmt.Errorf("引导作业 %q 的过期时间无效", id)
			}
			if job.State != platformadminapi.BootstrapPending && job.State != platformadminapi.BootstrapExpired && (job.ApprovedBy == "" || job.ApprovedBy == job.RequestedBy) {
				return fmt.Errorf("引导作业 %q 的审批身份无效", id)
			}
			if _, ok := state.Nodes[job.NodeID]; !ok {
				return fmt.Errorf("引导作业 %q 引用了不存在的节点", id)
			}
		}
		for id, binding := range state.TestBindings {
			if id == "" || binding.ID != id || binding.Version < 1 || validateTestBindingShape(binding) != nil {
				return fmt.Errorf("deployment-manager 状态包含无效测试目标绑定 %q", id)
			}
		}
		for _, release := range state.TestReleases {
			if release.ID == 0 || release.BindingID == "" || release.RequestedBy == "" || !validTestReleaseStatus(release.Status) {
				return fmt.Errorf("deployment-manager 状态包含无效测试发布 %d", release.ID)
			}
		}
		for id, candidate := range state.InstallationCandidates {
			if err := validateInstallationCandidateRecord(state, id, candidate); err != nil {
				return fmt.Errorf("deployment-manager 状态包含无效插件安装候选 %q: %w", id, err)
			}
		}
	}
	return nil
}

func (s *Service) saveLocked() error {
	if s.session == nil {
		if s.testSave != nil {
			return s.testSave(s.data)
		}
		return errors.New("Deployment Manager 写入缺少 Shared State 会话")
	}
	value := s.data.Tenants[s.session.tenant]
	if value == nil {
		return errors.New("Deployment Manager 写入缺少 tenant 聚合")
	}
	revision, err := s.session.repository.save(s.session.ctx, s.session.call, value, s.session.revision)
	if err != nil {
		return err
	}
	s.session.revision = revision
	return nil
}

func (s *Service) recoverInterruptedLocked() bool {
	changed := false
	now := s.now().Format(time.RFC3339Nano)
	for _, state := range s.data.Tenants {
		for id, job := range state.Jobs {
			if job.State != platformadminapi.BootstrapConnecting && job.State != platformadminapi.BootstrapInstalling {
				continue
			}
			job.State = platformadminapi.BootstrapFailed
			job.ErrorCode = "platform.deployment.interrupted"
			job.UpdatedAt = now
			state.Jobs[id] = job
			changed = true
		}
		for i := range state.TestReleases {
			release := &state.TestReleases[i]
			if testReleaseTerminal(release.Status) {
				continue
			}
			release.Status = platformadminapi.TestReleaseFailed
			release.ErrorCode = "platform.test_release.interrupted"
			release.ErrorMessage = "发布控制器重启中断了测试发布；如候选已经激活，必须显式执行恢复回滚"
			release.RollbackRequired = release.CandidateServiceRevisionID != 0
			release.UpdatedAt = now
			changed = true
		}
		// Reconcile exact repository references after every controller restart.
		// The transition snapshot already protects both old and new artifacts, so
		// marking the active revision pending can only retain extra bytes.
		for i := range state.Revisions {
			revision := &state.Revisions[i]
			if revision.Status == platformadminapi.ServicePublished && revision.Active && !revision.ReferencePending {
				revision.ReferencePending = true
				revision.UpdatedAt = now
				changed = true
			}
		}
	}
	return changed
}

func (s *Service) withTenantState(ctx context.Context, host sdk.Host, call *contractv1.CallContext, work func() error) error {
	tenant, err := callTenant(call)
	if err != nil {
		return err
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if s.testSave != nil {
		return work()
	}
	repository, err := newDeploymentStateRepository(host)
	if err != nil {
		return err
	}
	value, revision, err := repository.load(ctx, call)
	if err != nil {
		return err
	}
	s.data = persisted{Tenants: map[string]*tenantState{tenant: value}}
	s.session = &deploymentStateSession{ctx: ctx, call: call, repository: repository, tenant: tenant, revision: revision}
	if err := s.validateLoaded(); err != nil {
		s.closeStateSession()
		return err
	}
	if !s.recoveredTenants[tenant] {
		s.mu.Lock()
		changed := s.recoverInterruptedLocked()
		if changed {
			err = s.saveLocked()
		}
		s.mu.Unlock()
		if err != nil {
			s.closeStateSession()
			return err
		}
		s.recoveredTenants[tenant] = true
	}
	defer s.closeStateSession()
	return work()
}

func (s *Service) closeStateSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.session = nil
	s.data = persisted{Tenants: map[string]*tenantState{}}
}

func validJobState(state platformadminapi.BootstrapJobState) bool {
	switch state {
	case platformadminapi.BootstrapPending, platformadminapi.BootstrapApproved, platformadminapi.BootstrapConnecting,
		platformadminapi.BootstrapInstalling, platformadminapi.BootstrapSystemdActive, platformadminapi.BootstrapReady,
		platformadminapi.BootstrapFailed, platformadminapi.BootstrapExpired:
		return true
	default:
		return false
	}
}

func (s *Service) tenantLocked(tenant string) *tenantState {
	state := s.data.Tenants[tenant]
	if state == nil {
		state = emptyTenantState()
		s.data.Tenants[tenant] = state
	}
	if state.Nodes == nil {
		state.Nodes = map[string]platformadminapi.ManagedNode{}
	}
	if state.Jobs == nil {
		state.Jobs = map[string]platformadminapi.BootstrapJob{}
	}
	if state.TestBindings == nil {
		state.TestBindings = map[string]platformadminapi.TestTargetBinding{}
	}
	if state.InstallationCandidates == nil {
		state.InstallationCandidates = map[string]installationCandidateRecord{}
	}
	if state.ConfigurationRequests == nil {
		state.ConfigurationRequests = map[string]string{}
	}
	if state.ProfileActivations == nil {
		state.ProfileActivations = map[string]profileActivationRecord{}
	}
	return state
}

func emptyTenantState() *tenantState {
	return &tenantState{
		Nodes: map[string]platformadminapi.ManagedNode{}, Jobs: map[string]platformadminapi.BootstrapJob{},
		TestBindings: map[string]platformadminapi.TestTargetBinding{}, ConfigurationRequests: map[string]string{},
		ProfileActivations: map[string]profileActivationRecord{}, InstallationCandidates: map[string]installationCandidateRecord{},
	}
}

func validConfigurationRequestHash(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validServiceRevisionState(state platformadminapi.ServiceRevisionStatus) bool {
	switch state {
	case platformadminapi.ServiceDraft, platformadminapi.ServicePendingApproval, platformadminapi.ServiceApproved, platformadminapi.ServicePublishing, platformadminapi.ServicePublished:
		return true
	default:
		return false
	}
}

func callTenant(call *contractv1.CallContext) (string, error) {
	if call == nil || strings.TrimSpace(call.GetTenantId()) == "" {
		return "", errInvalid
	}
	return call.GetTenantId(), nil
}

func actor(call *contractv1.CallContext) (string, error) {
	if call == nil {
		return "", errInvalid
	}
	value := call.GetPrincipal().GetUserId()
	if value == "" {
		value = call.GetCaller().GetId()
	}
	if strings.TrimSpace(value) == "" {
		return "", errInvalid
	}
	return value, nil
}

func clonePlan(plan nodebootstrap.Plan) nodebootstrap.Plan {
	plan.SecretFiles = append([]nodebootstrap.CredentialSecretFile(nil), plan.SecretFiles...)
	return plan
}

func cloneNode(node platformadminapi.ManagedNode) platformadminapi.ManagedNode {
	node.Plan = clonePlan(node.Plan)
	return node
}
