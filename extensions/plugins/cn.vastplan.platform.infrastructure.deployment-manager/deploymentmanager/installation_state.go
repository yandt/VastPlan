package deploymentmanager

import (
	"errors"
	"reflect"
	"sort"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

type installationCandidateRecord struct {
	ID                        string                            `json:"id"`
	Source                    plugininstallation.Source         `json:"source"`
	Request                   plugininstallation.PreviewRequest `json:"request"`
	Preview                   plugininstallation.Preview        `json:"preview"`
	ServiceRevisionID         uint64                            `json:"serviceRevisionId"`
	PreviousServiceRevisionID uint64                            `json:"previousServiceRevisionId"`
	RequestedBy               string                            `json:"requestedBy"`
	CancelledBy               string                            `json:"cancelledBy,omitempty"`
	CancelledAt               string                            `json:"cancelledAt,omitempty"`
	CreatedAt                 string                            `json:"createdAt"`
	UpdatedAt                 string                            `json:"updatedAt"`
	Migration                 plugininstallation.MigrationState `json:"migration"`
}

type serviceRevisionWorkflowOwner string

const (
	revisionOwnerOrdinary      serviceRevisionWorkflowOwner = "ordinary"
	revisionOwnerConfiguration serviceRevisionWorkflowOwner = "configuration"
	revisionOwnerInstallation  serviceRevisionWorkflowOwner = "installation"
)

func requireServiceRevisionOwner(state *tenantState, revisionID uint64, owner serviceRevisionWorkflowOwner) error {
	installationOwned := false
	for _, candidate := range state.InstallationCandidates {
		if candidate.CancelledAt == "" && candidate.ServiceRevisionID == revisionID {
			installationOwned = true
			break
		}
	}
	if installationOwned != (owner == revisionOwnerInstallation) {
		return errServiceState
	}
	return nil
}

func validateInstallationCandidateRecord(state *tenantState, id string, record installationCandidateRecord) error {
	normalizedRequest, requestErr := plugininstallation.ValidatePreviewRequest(record.Request)
	if id == "" || record.ID != id || !plugininstallation.ValidSource(record.Source) || record.RequestedBy == "" ||
		record.ServiceRevisionID == 0 || record.PreviousServiceRevisionID == 0 ||
		record.Preview.Version != plugininstallation.ProtocolVersion || record.Preview.Source != record.Source ||
		record.Preview.CandidateRevision != record.ServiceRevisionID || record.Preview.ActiveRevision != record.PreviousServiceRevisionID ||
		requestErr != nil || !reflect.DeepEqual(normalizedRequest, record.Request) {
		return errors.New("插件安装候选身份无效")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.CreatedAt); err != nil {
		return errors.New("插件安装候选创建时间无效")
	}
	if _, err := time.Parse(time.RFC3339Nano, record.UpdatedAt); err != nil {
		return errors.New("插件安装候选更新时间无效")
	}
	if record.CancelledAt != "" {
		if record.CancelledBy == "" {
			return errors.New("插件安装候选取消主体无效")
		}
		if _, err := time.Parse(time.RFC3339Nano, record.CancelledAt); err != nil {
			return errors.New("插件安装候选取消时间无效")
		}
		if _, err := serviceRevisionByID(state, record.ServiceRevisionID); !errors.Is(err, errNotFound) {
			return errors.New("已取消插件安装候选仍残留服务草稿")
		}
		return nil
	}
	revision, err := serviceRevisionByID(state, record.ServiceRevisionID)
	if err != nil || revision.Deployment != record.Request.Target.Deployment || revision.Intent == nil ||
		revision.Intent.Digest() != record.Preview.CandidateIntentDigest {
		return errors.New("插件安装候选与服务修订不一致")
	}
	return nil
}

func projectInstallationCandidate(state *tenantState, record installationCandidateRecord) (plugininstallation.Candidate, error) {
	result := plugininstallation.Candidate{
		ID: record.ID, Source: record.Source, Preview: cloneJSON(record.Preview),
		ServiceRevisionID: record.ServiceRevisionID, PreviousServiceRevisionID: record.PreviousServiceRevisionID,
		RequestedBy: record.RequestedBy, CancelledBy: record.CancelledBy,
		CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt,
		Migration: record.Migration,
	}
	if record.CancelledAt != "" {
		result.Status = plugininstallation.CandidateCancelled
		return result, nil
	}
	revision, err := serviceRevisionByID(state, record.ServiceRevisionID)
	if err != nil {
		return plugininstallation.Candidate{}, err
	}
	result.SubmittedBy, result.ApprovedBy, result.ActivatedBy = revision.SubmittedBy, revision.ApprovedBy, revision.PublishedBy
	if result.Preview.ArtifactLock == nil && revision.ResolutionReport != nil {
		result.Preview.ArtifactLock = cloneArtifactLock(revision.ResolutionReport.ArtifactLock)
	}
	if revision.UpdatedAt > result.UpdatedAt {
		result.UpdatedAt = revision.UpdatedAt
	}
	if revision.PlanningStale {
		result.Status = plugininstallation.CandidateStale
		return result, nil
	}
	switch revision.Status {
	case platformadminapi.ServiceDraft:
		result.Status = plugininstallation.CandidatePlanned
	case platformadminapi.ServicePendingApproval:
		result.Status = plugininstallation.CandidatePendingApproval
	case platformadminapi.ServiceApproved:
		result.Status = plugininstallation.CandidateApproved
	case platformadminapi.ServicePublishing:
		result.Status = plugininstallation.CandidateActivating
	case platformadminapi.ServicePublished:
		if revision.Active {
			result.Status = plugininstallation.CandidateReady
		} else if rollback, ok := inferredInstallationRollback(state, record); ok {
			result.Status, result.RollbackServiceRevisionID = plugininstallation.CandidateRolledBack, rollback.ID
			if rollback.UpdatedAt > result.UpdatedAt {
				result.UpdatedAt = rollback.UpdatedAt
			}
		} else {
			result.Status = plugininstallation.CandidateSuperseded
		}
	default:
		return plugininstallation.Candidate{}, errServiceState
	}
	return result, nil
}

func inferredInstallationRollback(state *tenantState, record installationCandidateRecord) (platformadminapi.ServiceRevision, bool) {
	previous, err := serviceRevisionByID(state, record.PreviousServiceRevisionID)
	if err != nil {
		return platformadminapi.ServiceRevision{}, false
	}
	active, err := activeServiceRevision(state, record.Request.Target.Deployment)
	if err != nil || active.ID <= record.ServiceRevisionID || active.PreviousServiceRevision != previous.ID {
		return platformadminapi.ServiceRevision{}, false
	}
	return active, true
}

func sortedInstallationCandidates(state *tenantState) ([]plugininstallation.Candidate, error) {
	items := make([]plugininstallation.Candidate, 0, len(state.InstallationCandidates))
	for _, record := range state.InstallationCandidates {
		item, err := projectInstallationCandidate(state, record)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].ID < items[j].ID
	})
	return items, nil
}
