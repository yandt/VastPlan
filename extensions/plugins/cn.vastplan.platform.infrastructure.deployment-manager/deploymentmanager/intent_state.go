package deploymentmanager

import (
	"errors"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
)

func validateServiceRevisionRecord(tenant string, revision platformadminapi.ServiceRevision) error {
	if revision.ID == 0 || revision.Deployment == "" || !validServiceRevisionState(revision.Status) {
		return errors.New("服务修订身份或状态无效")
	}
	if !isIntentRevision(revision) {
		if revision.Composition.Metadata.Tenant != tenant || revision.Composition.Metadata.Name != revision.Deployment || revision.PreviewDigest == "" {
			return errors.New("内部 Application Composition 修订无效")
		}
		return nil
	}
	if revision.Intent == nil || revision.ResolutionReport == nil || revision.Intent.Metadata.Tenant != tenant || revision.Intent.Metadata.Name != revision.Deployment || revision.Intent.Revision != revision.ID {
		return errors.New("Application Intent 修订身份无效")
	}
	intent, err := backendcompositionv1.ValidateApplicationIntent(*revision.Intent)
	if err != nil || intent.Digest() != revision.ResolutionReport.Intent.Digest {
		return errors.New("Application Intent 与规划快照不一致")
	}
	report, err := backendcompositionv1.ValidateResolutionReport(*revision.ResolutionReport)
	if err != nil || report.Intent.ID != intent.ID || report.Intent.Revision != intent.Revision {
		return errors.New("Resolution Report 无效")
	}
	if revision.ConfigurationSnapshot != nil && (revision.ConfigurationSnapshot.Version != 1 || revision.ConfigurationSnapshot.Digest != revision.ConfigurationSnapshot.ComputedDigest()) {
		return errors.New("可信配置快照无效")
	}
	if report.Status == backendcompositionv1.ResolutionResolved {
		if revision.PreviewDigest == "" || revision.Composition.Metadata.Tenant != tenant || revision.Composition.Metadata.Name != revision.Deployment || revision.Composition.Digest() != report.ApplicationCompositionDigest {
			return errors.New("Resolved 规划与内核预览不一致")
		}
	} else if revision.Status != platformadminapi.ServiceDraft || revision.PreviewDigest != "" {
		return errors.New("未收敛的 Intent 只能保留为无预览草稿")
	}
	if revision.PlanningStale && (revision.Status != platformadminapi.ServiceDraft || revision.SubmittedPlanDigest != "" || revision.ApprovedPlanDigest != "" || len(revision.ObservedPlanDigest) != 64) {
		return errors.New("stale Intent 没有撤销审批摘要")
	}
	if revision.Status != platformadminapi.ServiceDraft && (revision.SubmittedPlanDigest == "" || revision.SubmittedPlanDigest != report.PlanDigest) {
		return errors.New("已提交修订未绑定计划摘要")
	}
	if (revision.Status == platformadminapi.ServiceApproved || revision.Status == platformadminapi.ServicePublishing || revision.Status == platformadminapi.ServicePublished) && (revision.ApprovedPlanDigest == "" || revision.ApprovedPlanDigest != report.PlanDigest) {
		return errors.New("已审批修订未绑定计划摘要")
	}
	return nil
}
