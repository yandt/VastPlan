package deploymentmanager

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/configurationactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/nodebootstrap"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type handlerRequest struct {
	ID                    string                                             `json:"id"`
	NodeID                string                                             `json:"nodeId"`
	JobID                 string                                             `json:"jobId"`
	Plan                  nodebootstrap.Plan                                 `json:"plan"`
	IfVersion             *int64                                             `json:"ifVersion,omitempty"`
	RevisionID            uint64                                             `json:"revisionId"`
	ReleaseID             uint64                                             `json:"releaseId"`
	Intent                backendcompositionv1.ApplicationIntent             `json:"intent"`
	ConfigurationSnapshot backendcompositionv1.PlanningConfigurationSnapshot `json:"configurationSnapshot"`
	Binding               platformadminapi.PutTestTargetBindingRequest       `json:"binding"`
	Release               platformadminapi.CreateTestReleaseRequest          `json:"release"`
	Activation            configurationactivation.CreateRequest              `json:"activation"`
	ProfileActivation     platformprofileactivation.CreateActivationRequest  `json:"profileActivation"`
	CandidateID           string                                             `json:"candidateId"`
	InstallationPreview   plugininstallation.PreviewRequest                  `json:"installationPreview"`
	InstallationTarget    plugininstallation.Target                          `json:"installationTarget"`
	ApprovalEvidence      map[string]json.RawMessage                         `json:"approvalEvidence,omitempty"`
}

func (s *Service) dispatchOperation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request handlerRequest) (out any, err error) {
	switch operation {
	case "listNodes":
		var items []platformadminapi.ManagedNode
		items, err = s.ListNodes(call)
		out = map[string]any{"items": items}
	case "putNode":
		out, err = s.PutNode(call, request.ID, platformadminapi.PutManagedNodeRequest{Plan: request.Plan, IfVersion: request.IfVersion})
	case "listBootstrapJobs":
		_ = s.refreshReadiness(ctx, host, call)
		var items []platformadminapi.BootstrapJob
		items, err = s.ListJobs(call)
		out = map[string]any{"items": items}
	case "createBootstrap":
		out, err = s.CreateJob(call, request.NodeID)
	case "approveBootstrap":
		out, err = s.dispatchBootstrapApproval(ctx, host, call, request.JobID)
	case "listDeploymentTargets":
		var items []platformadminapi.DeploymentTarget
		items, err = s.ListDeploymentTargets(ctx, host, call)
		out = map[string]any{"items": items}
	case "listServiceRevisions":
		_ = s.ReconcileServiceReferences(ctx, host, call)
		var items []platformadminapi.ServiceRevision
		items, err = s.ListServiceRevisions(call)
		out = map[string]any{"items": publicServiceRevisions(items)}
	case plugininstallation.PreviewOperation, plugininstallation.SelfServicePreviewOperation, plugininstallation.DevelopmentPreviewOperation:
		out, err = s.PreviewPluginInstallation(ctx, host, call, installationSource(operation), request.InstallationPreview)
	case plugininstallation.CreateOperation, plugininstallation.SelfServiceCreateOperation, plugininstallation.DevelopmentCreateOperation:
		out, err = s.CreatePluginInstallationCandidate(ctx, host, call, installationSource(operation), request.InstallationPreview)
	case plugininstallation.DevelopmentApplyOperation:
		out, err = s.ApplyDevelopmentPluginInstallation(ctx, host, call, request.InstallationPreview)
	case plugininstallation.ListTargetsOperation:
		var items []plugininstallation.TargetOption
		items, err = s.ListPluginInstallationTargets(call)
		out = map[string]any{"items": items}
	case plugininstallation.ListOperation:
		var items []plugininstallation.Candidate
		items, err = s.ListPluginInstallationCandidates(call)
		if err == nil {
			items = observeInstallationRollouts(ctx, host, call, items)
		}
		out = map[string]any{"items": items}
	case plugininstallation.SelfServiceListOperation:
		var items []plugininstallation.Candidate
		items, err = s.ListSelfServicePluginInstallationCandidates(call, request.InstallationTarget)
		if err == nil {
			items = observeInstallationRollouts(ctx, host, call, items)
		}
		out = map[string]any{"items": items}
	case plugininstallation.GetOperation:
		var candidate plugininstallation.Candidate
		candidate, err = s.GetPluginInstallationCandidate(call, request.CandidateID)
		out = observeInstallationRollout(ctx, host, call, candidate)
	case plugininstallation.SelfServiceGetOperation:
		var candidate plugininstallation.Candidate
		candidate, err = s.GetSelfServicePluginInstallationCandidate(call, request.CandidateID, request.InstallationTarget)
		out = observeInstallationRollout(ctx, host, call, candidate)
	case plugininstallation.SubmitOperation:
		out, err = s.SubmitPluginInstallationCandidate(ctx, host, call, request.CandidateID)
	case plugininstallation.ApproveOperation:
		out, err = s.ApprovePluginInstallationCandidate(ctx, host, call, request.CandidateID)
	case plugininstallation.ActivateOperation:
		out, err = s.ActivatePluginInstallationCandidate(ctx, host, call, request.CandidateID)
	case plugininstallation.CancelOperation:
		out, err = s.CancelPluginInstallationCandidate(call, request.CandidateID)
	case plugininstallation.RollbackOperation:
		out, err = s.RollbackPluginInstallationCandidate(ctx, host, call, request.CandidateID)
	case plugininstallation.SelfServiceSubmitOperation:
		out, err = s.SubmitSelfServicePluginInstallationCandidate(ctx, host, call, request.CandidateID, request.InstallationTarget)
	case plugininstallation.SelfServiceApproveOperation:
		out, err = s.ApproveSelfServicePluginInstallationCandidate(ctx, host, call, request.CandidateID, request.InstallationTarget, request.ApprovalEvidence)
	case plugininstallation.SelfServiceActivateOperation:
		out, err = s.ActivateSelfServicePluginInstallationCandidate(ctx, host, call, request.CandidateID, request.InstallationTarget)
	case plugininstallation.SelfServiceCancelOperation:
		out, err = s.CancelSelfServicePluginInstallationCandidate(call, request.CandidateID, request.InstallationTarget)
	case plugininstallation.SelfServiceRollbackOperation:
		out, err = s.RollbackSelfServicePluginInstallationCandidate(ctx, host, call, request.CandidateID, request.InstallationTarget)
	case "createIntentDraft":
		out, err = s.CreateIntentDraft(ctx, host, call, request.Intent)
	case "updateIntentDraft":
		out, err = s.UpdateIntentDraft(ctx, host, call, request.RevisionID, request.Intent)
	case "refreshIntentDraft":
		out, err = s.RefreshIntentPlan(ctx, host, call, request.RevisionID)
	case "bindIntentConfiguration":
		out, err = s.BindIntentConfiguration(ctx, host, call, request.RevisionID, request.ConfigurationSnapshot)
	case "submitServiceDraft":
		out, err = s.SubmitServiceDraft(ctx, host, call, request.RevisionID)
	case "approveServiceRevision":
		out, err = s.ApproveServiceRevision(ctx, host, call, request.RevisionID)
	case "publishServiceRevision":
		out, err = s.PublishServiceRevision(ctx, host, call, request.RevisionID)
	case "rollbackServiceRevision":
		out, err = s.RollbackServiceRevision(ctx, host, call, request.RevisionID)
	case configurationactivation.CreateOperation:
		out, err = s.CreateConfigurationActivation(ctx, host, call, request.Activation)
	case configurationactivation.GetOperation:
		out, err = s.GetConfigurationActivation(ctx, host, call, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	case configurationactivation.PublishOperation:
		out, err = s.PublishConfigurationActivation(ctx, host, call, configurationactivation.LookupRequest{CandidateID: request.CandidateID})
	case platformprofileactivation.CreateActivationOperation:
		out, err = s.CreateProfileConfigurationActivation(ctx, host, call, request.ProfileActivation)
	case platformprofileactivation.GetActivationOperation:
		out, err = s.GetProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.ApproveActivationOperation:
		out, err = s.ApproveProfileConfigurationActivation(call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.PublishActivationOperation:
		out, err = s.PublishProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case platformprofileactivation.AbortActivationOperation:
		out, err = s.AbortProfileConfigurationActivation(ctx, host, call, platformprofileactivation.ActivationLookup{CandidateID: request.CandidateID})
	case "listServiceRevisionAudit":
		_ = s.ReconcileServiceReferences(ctx, host, call)
		var items []platformadminapi.ServiceAuditEvent
		items, err = s.ListServiceRevisionAudit(call, request.RevisionID)
		out = map[string]any{"items": items}
	case "listTestTargetBindings":
		var items []platformadminapi.TestTargetBinding
		items, err = s.ListTestTargetBindings(call)
		out = map[string]any{"items": items}
	case "putTestTargetBinding":
		out, err = s.PutTestTargetBinding(call, request.ID, request.Binding)
	case "listTestReleases":
		var items []platformadminapi.TestRelease
		items, err = s.ListTestReleases(call)
		out = map[string]any{"items": items}
	case "createTestRelease":
		out, err = s.CreateTestRelease(ctx, host, call, request.Release)
	case "rollbackTestRelease":
		out, err = s.RollbackTestRelease(ctx, host, call, request.ReleaseID)
	default:
		err = errInvalid
	}
	return out, err
}

func (s *Service) dispatchBootstrapApproval(ctx context.Context, host sdk.Host, call *contractv1.CallContext, jobID string) (platformadminapi.BootstrapJob, error) {
	job, node, err := s.beginApproval(call, jobID)
	if err != nil {
		return job, err
	}
	operation := "bootstrap"
	raw, err := json.Marshal(nodebootstrap.ExecutionRequest{OperationID: job.ID, Plan: node.Plan})
	if err != nil {
		return job, err
	}
	result, _, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: nodebootstrap.KernelService, Operation: &operation}, call, raw)
	success := callErr == nil && result != nil && result.Status == contractv1.CallResult_STATUS_OK
	job, err = s.finishApproval(call, job.ID, success)
	if success && err == nil {
		_ = s.refreshReadiness(ctx, host, call)
		job, err = s.job(call, job.ID)
	}
	if !success && err == nil {
		err = errBootstrapFailed
	}
	return job, err
}

func installationSource(operation string) plugininstallation.Source {
	switch operation {
	case plugininstallation.SelfServicePreviewOperation, plugininstallation.SelfServiceCreateOperation:
		return plugininstallation.SourceSelfService
	case plugininstallation.DevelopmentPreviewOperation, plugininstallation.DevelopmentCreateOperation, plugininstallation.DevelopmentApplyOperation:
		return plugininstallation.SourceDevelopment
	default:
		return plugininstallation.SourceController
	}
}
