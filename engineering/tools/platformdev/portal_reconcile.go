package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

// reconcilePlatformPortal is the retry-safe implementation behind the
// explicit --apply-platform command. Every durable lifecycle state is a valid
// resume point; a conflicting WorkingCopy or Publication is never overwritten.
func reconcilePlatformPortal(client *http.Client, baseURL string, desired portalapi.PortalConfiguration) error {
	governance, err := readPortalGovernance(client, baseURL)
	if err != nil {
		return err
	}
	portal, err := selectPlatformPortal(client, baseURL, findPortal(governance.Portals, desired.Application.ID), desired)
	if err != nil {
		return err
	}
	publication, err := resumePortalPublication(client, baseURL, portal)
	if err != nil {
		return err
	}

	governance, err = readPortalGovernance(client, baseURL)
	if err != nil {
		return err
	}
	current := findPortal(governance.Portals, desired.Application.ID)
	if current == nil {
		return errors.New("发布后 Portal 聚合不存在")
	}
	for _, release := range current.Releases {
		if release.PublicationID != publication.ID {
			continue
		}
		if release.Status == portalapi.ActivationCurrent {
			return nil
		}
		if release.Status == portalapi.ActivationSuperseded {
			return errors.New("目标 Publication 已进入历史上线记录；重新启用必须走显式 rollback")
		}
	}
	request := portalapi.PortalPublicationReleaseRequest{PublicationID: publication.ID, ExpectedCurrentReleaseID: current.CurrentReleaseID, Reason: "platformdev explicit release"}
	status, raw, err := portalRequest(client, baseURL, publisherToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/releases", desired.Application.ID), request, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("release status=%d body=%s: %w", status, raw, err)
	}
	var release portalapi.PortalRelease
	if err := json.Unmarshal(raw, &release); err != nil {
		return fmt.Errorf("decode release: %w", err)
	}
	if release.Status != portalapi.ActivationCurrent {
		return fmt.Errorf("Portal release failed: %+v", release)
	}
	return nil
}

func selectPlatformPortal(client *http.Client, baseURL string, portal *portalapi.Portal, desired portalapi.PortalConfiguration) (*portalapi.Portal, error) {
	if portal == nil {
		status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, "/v1/portals", portalapi.CreatePortalRequest{PortalID: desired.Application.ID, Configuration: desired}, true)
		if err != nil || status != http.StatusOK {
			return nil, fmt.Errorf("create Portal status=%d body=%s: %w", status, raw, err)
		}
		var created portalapi.Portal
		if err := json.Unmarshal(raw, &created); err != nil || created.WorkingCopy == nil {
			return nil, fmt.Errorf("Composer 未返回有效 Portal WorkingCopy: %w", err)
		}
		return &created, nil
	}
	configuration, status := currentPortalConfiguration(portal)
	if configuration == nil {
		return nil, errors.New("现有 Portal 没有可管理配置")
	}
	if samePortalConfiguration(*configuration, desired) {
		return portal, nil
	}
	if portal.WorkingCopy != nil || portal.PendingPublication != nil {
		return nil, fmt.Errorf("Portal 已有内容不同的未完成 %s，拒绝自动覆盖", status)
	}
	if portal.PublishedPublication == nil {
		return nil, errors.New("现有 Portal 没有可复制的 Published Publication")
	}
	statusCode, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/working-copy", desired.Application.ID), map[string]any{"configuration": desired}, true)
	if err != nil || statusCode != http.StatusOK {
		return nil, fmt.Errorf("create WorkingCopy status=%d body=%s: %w", statusCode, raw, err)
	}
	var working portalapi.PortalWorkingCopy
	if err := json.Unmarshal(raw, &working); err != nil || working.Revision == 0 {
		return nil, fmt.Errorf("Composer 未返回有效 WorkingCopy: %w", err)
	}
	portal.WorkingCopy = &working
	return portal, nil
}

func resumePortalPublication(client *http.Client, baseURL string, portal *portalapi.Portal) (portalapi.PortalPublication, error) {
	if portal.WorkingCopy != nil {
		status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/publications", portal.ID), portalapi.SubmitPortalPublicationRequest{ExpectedWorkingRevision: portal.WorkingCopy.Revision}, true)
		if err != nil || status != http.StatusOK {
			return portalapi.PortalPublication{}, fmt.Errorf("submit status=%d body=%s: %w", status, raw, err)
		}
		var publication portalapi.PortalPublication
		if err := json.Unmarshal(raw, &publication); err != nil {
			return portalapi.PortalPublication{}, fmt.Errorf("decode submit Publication: %w", err)
		}
		portal.WorkingCopy, portal.PendingPublication = nil, &publication
	}
	publication := portal.PendingPublication
	if publication == nil {
		publication = portal.PublishedPublication
	}
	if publication == nil {
		return portalapi.PortalPublication{}, errors.New("Portal 没有可继续的 Publication")
	}
	steps := []struct {
		from, to portalapi.Status
		token    string
		action   string
	}{{portalapi.StatusPendingApproval, portalapi.StatusApproved, approverToken, "approve"}, {portalapi.StatusApproved, portalapi.StatusPublished, publisherToken, "publish"}}
	for _, step := range steps {
		if publication.Status != step.from {
			continue
		}
		path := fmt.Sprintf("/v1/portals/%s/publications/%d/%s", portal.ID, publication.ID, step.action)
		status, raw, err := portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return portalapi.PortalPublication{}, fmt.Errorf("%s status=%d body=%s: %w", step.action, status, raw, err)
		}
		if err := json.Unmarshal(raw, publication); err != nil {
			return portalapi.PortalPublication{}, fmt.Errorf("decode %s Publication: %w", step.action, err)
		}
		if publication.Status != step.to {
			return portalapi.PortalPublication{}, fmt.Errorf("%s 后 Publication 状态错误: %s", step.action, publication.Status)
		}
	}
	if publication.Status != portalapi.StatusPublished {
		return portalapi.PortalPublication{}, fmt.Errorf("Publication 无法从状态 %s 继续发布", publication.Status)
	}
	return *publication, nil
}

func currentPortalConfiguration(portal *portalapi.Portal) (*portalapi.PortalConfiguration, string) {
	if portal.WorkingCopy != nil {
		return &portal.WorkingCopy.Configuration, "WorkingCopy"
	}
	if portal.PendingPublication != nil && portal.PendingPublication.Source.Configuration != nil {
		return portal.PendingPublication.Source.Configuration, "Publication"
	}
	if portal.PublishedPublication != nil && portal.PublishedPublication.Source.Configuration != nil {
		return portal.PublishedPublication.Source.Configuration, "Published Publication"
	}
	return nil, "configuration"
}

func readPortalGovernance(client *http.Client, baseURL string) (portalapi.PortalGovernanceSnapshot, error) {
	status, raw, err := portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portals", nil, false)
	if err != nil || status != http.StatusOK {
		return portalapi.PortalGovernanceSnapshot{}, fmt.Errorf("governance status=%d body=%s: %w", status, raw, err)
	}
	var governance portalapi.PortalGovernanceSnapshot
	if err := json.Unmarshal(raw, &governance); err != nil {
		return portalapi.PortalGovernanceSnapshot{}, fmt.Errorf("decode governance: %w", err)
	}
	return governance, nil
}

func findPortal(portals []portalapi.Portal, id string) *portalapi.Portal {
	for index := range portals {
		if portals[index].ID == id {
			return &portals[index]
		}
	}
	return nil
}

func samePortalApplication(current, desired frontendcompositionv1.ApplicationComposition) bool {
	desired.Document = current.Document
	desired.Target = current.Target
	return current.Digest() == desired.Digest()
}

func samePortalConfiguration(current, desired portalapi.PortalConfiguration) bool {
	if !samePortalApplication(current.Application, desired.Application) || current.Platform.Digest() != desired.Platform.Digest() {
		return false
	}
	currentServices, currentErr := json.Marshal(current.Services)
	desiredServices, desiredErr := json.Marshal(desired.Services)
	return currentErr == nil && desiredErr == nil && bytes.Equal(currentServices, desiredServices)
}
