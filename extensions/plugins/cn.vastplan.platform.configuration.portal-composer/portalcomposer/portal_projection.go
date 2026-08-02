package portalcomposer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func (s *Service) workingCopyIndexLocked(tenantID, portalID string) (int, error) {
	index := -1
	for candidate := range s.state.Revisions {
		revision := s.state.Revisions[candidate]
		if revision.TenantID != tenantID || revision.PortalID != portalID || revision.Status != portalapi.StatusDraft || s.isHiddenVersionLocked(revision.ID) {
			continue
		}
		if index >= 0 {
			return 0, errors.New("Portal 存在多个 WorkingCopy")
		}
		index = candidate
	}
	if index < 0 {
		return 0, ErrNotFound
	}
	return index, nil
}

func (s *Service) portalWorkingCopyLocked(tenantID string, revision portalapi.Revision) (portalapi.PortalWorkingCopy, error) {
	if revision.Status != portalapi.StatusDraft || revision.WorkingRevision == 0 {
		return portalapi.PortalWorkingCopy{}, ErrInvalidState
	}
	configuration, err := s.portalConfigurationLocked(tenantID, revision)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	digest, err := portalConfigurationDigest(configuration)
	if err != nil {
		return portalapi.PortalWorkingCopy{}, err
	}
	return portalapi.PortalWorkingCopy{
		TenantID: revision.TenantID, PortalID: revision.PortalID, Revision: revision.WorkingRevision,
		Configuration: configuration, Digest: digest, UpdatedBy: revision.UpdatedBy, CreatedAt: revision.CreatedAt, UpdatedAt: revision.UpdatedAt,
	}, nil
}

func (s *Service) portalPublicationLocked(tenantID string, revision portalapi.Revision) (portalapi.PortalPublication, error) {
	if revision.Status == portalapi.StatusDraft || revision.ConfigurationDigest == "" || revision.SubmittedBy == "" || revision.SubmittedAt == "" {
		return portalapi.PortalPublication{}, ErrInvalidState
	}
	configuration, err := s.portalConfigurationLocked(tenantID, revision)
	if err != nil {
		return portalapi.PortalPublication{}, err
	}
	digest, err := portalConfigurationDigest(configuration)
	if err != nil || digest != revision.ConfigurationDigest {
		return portalapi.PortalPublication{}, errors.New("Portal Publication 冻结内容摘要无效")
	}
	source := portalapi.PortalPublicationSource{Kind: portalapi.PortalPublicationSourceInline, Configuration: &configuration}
	if control, enabled := s.state.VersionControls[revision.PortalID]; enabled {
		for _, record := range control.History {
			entry := record.Entry
			if entry.PublicationID != revision.ID {
				continue
			}
			ref := entry.VersionRef
			source = portalapi.PortalPublicationSource{
				Kind: portalapi.PortalPublicationSourceWorkspace, Configuration: &configuration,
				EnvironmentID: entry.EnvironmentID, EnvironmentDigest: entry.EnvironmentDigest, VersionRef: &ref,
			}
			break
		}
	}
	return portalapi.PortalPublication{
		ID: revision.ID, TenantID: revision.TenantID, PortalID: revision.PortalID, WorkingRevision: revision.WorkingRevision,
		Status: revision.Status, Digest: digest, Source: source,
		Resolved: cloneSpec(revision.Spec), SubmittedBy: revision.SubmittedBy, ApprovedBy: revision.ApprovedBy, PublishedBy: revision.PublishedBy,
		CreatedAt: revision.SubmittedAt, UpdatedAt: revision.UpdatedAt,
	}, nil
}

func (s *Service) portalConfigurationLocked(tenantID string, revision portalapi.Revision) (portalapi.PortalConfiguration, error) {
	profileIndex, bindingIndex, err := s.versionPartsLocked(tenantID, revision)
	if err != nil {
		return portalapi.PortalConfiguration{}, err
	}
	return portalapi.PortalConfiguration{
		Platform: cloneJSON(s.state.Profiles[profileIndex].Profile), Application: cloneComposition(revision.Composition),
		Services: cloneJSON(s.state.Bindings[bindingIndex].Binding.Services),
	}, nil
}

func portalConfigurationDigest(configuration portalapi.PortalConfiguration) (string, error) {
	raw, err := json.Marshal(configuration)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}
