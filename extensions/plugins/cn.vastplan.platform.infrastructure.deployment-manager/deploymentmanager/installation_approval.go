package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	approvalv2 "cdsoft.com.cn/VastPlan/contracts/schemas/approval/v2"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	approvalsdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/approvalpolicy"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var (
	errInstallationApprovalDenied   = errors.New("插件安装审批策略拒绝当前变更")
	errInstallationApprovalRequired = errors.New("插件安装审批策略要求补充证据")
)

func (s *Service) evaluateInstallationApproval(ctx context.Context, host sdk.Host, call *contractv1.CallContext, preview plugininstallation.Preview, submittedBy string, evidence map[string]json.RawMessage) (*approvalv2.Decision, error) {
	if preview.Source == plugininstallation.SourceDevelopment || s.approvalBinding == nil || preview.Source != plugininstallation.SourceSelfService {
		return nil, nil
	}
	if call == nil || call.GetPrincipal().GetUserId() == "" {
		return nil, errors.New("插件安装审批缺少可信主体")
	}
	client, err := approvalsdk.New(host, *s.approvalBinding)
	if err != nil {
		return nil, err
	}
	input := approvalv2.EvaluationInput{
		Operation: "plugin.installation.activate", TenantID: call.GetTenantId(),
		Actor: approvalv2.ActorFacts{ID: call.GetPrincipal().GetUserId(), Roles: append([]string(nil), call.GetPrincipal().GetSystemRoles()...)},
		Resource: approvalv2.ResourceFacts{
			ID:     fmt.Sprintf("%s/%s/%d", preview.Target.Deployment, preview.Target.UnitID, preview.CandidateRevision),
			Digest: preview.PlanDigest, SubmittedBy: submittedBy,
			Attributes: map[string]string{"pluginId": preview.PluginID, "publisher": installationPublisher(preview), "action": string(preview.Action)},
		},
		Context: map[string]string{"source": string(preview.Source)}, Evidence: evidence,
	}
	decision, err := client.Evaluate(ctx, call, input)
	if err != nil {
		return nil, err
	}
	if decision.Status == approvalv2.DecisionDenied {
		return &decision, fmt.Errorf("%w: %s", errInstallationApprovalDenied, decision.Message)
	}
	return &decision, nil
}

func installationPublisher(preview plugininstallation.Preview) string {
	if preview.ArtifactLock == nil {
		return ""
	}
	for _, item := range preview.ArtifactLock.Packages {
		if item.Ref.PluginID == preview.PluginID {
			return item.Publisher
		}
	}
	return ""
}

// approveInstallationByPolicy resumes safely after a crash between submission
// and policy approval. It never reuses a user identity as the approver.
func (s *Service) approveInstallationByPolicy(call *contractv1.CallContext, id string) error {
	tenant, err := callTenant(call)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.tenantLocked(tenant)
	record, ok := state.InstallationCandidates[id]
	if !ok || record.CancelledAt != "" || record.Preview.Impact.RequiresApproval {
		return errServiceState
	}
	if record.Source != plugininstallation.SourceDevelopment && (record.Preview.Approval == nil || record.Preview.Approval.Status != approvalv2.DecisionAllowed) {
		return errServiceState
	}
	index, err := serviceRevisionIndex(state, record.ServiceRevisionID)
	if err != nil {
		return err
	}
	revision := state.Revisions[index]
	if revision.Status == platformadminapi.ServiceApproved {
		return nil
	}
	if revision.Status != platformadminapi.ServicePendingApproval || revision.SubmittedPlanDigest == "" || revision.ResolutionReport == nil || revision.SubmittedPlanDigest != revision.ResolutionReport.PlanDigest || revision.SubmittedPlanDigest != record.Preview.PlanDigest {
		return errServiceState
	}
	approver := "policy:development"
	if record.Preview.Approval != nil {
		approver = fmt.Sprintf("policy:%s@%d", record.Preview.Approval.Profile.ID, record.Preview.Approval.Profile.Revision)
	}
	old := revision
	revision.Status, revision.ApprovedBy = platformadminapi.ServiceApproved, approver
	revision.ApprovedPlanDigest, revision.UpdatedAt = revision.ResolutionReport.PlanDigest, s.now().Format(time.RFC3339Nano)
	state.Revisions[index] = revision
	oldAuditLength, oldNextAudit := len(state.ServiceAudit), state.NextAudit
	s.auditServiceLocked(state, revision, "service.installation_candidate.policy_approved", approver)
	if err := s.saveLocked(); err != nil {
		state.Revisions[index] = old
		state.ServiceAudit, state.NextAudit = state.ServiceAudit[:oldAuditLength], oldNextAudit
		return err
	}
	return nil
}
