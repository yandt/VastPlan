package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

// reconcilePlatformPortal is the retry-safe implementation behind the
// explicit --apply-platform command. Every durable lifecycle state is a valid
// resume point; a conflicting open draft is never overwritten automatically.
func reconcilePlatformPortal(client *http.Client, baseURL string, desired frontendcompositionv1.ApplicationComposition) error {
	governance, err := readPortalGovernance(client, baseURL)
	if err != nil {
		return err
	}
	portal := findPortal(governance.Portals, desired.ID)
	version, err := selectPlatformPortalVersion(client, baseURL, portal, desired)
	if err != nil {
		return err
	}
	if version.ID == 0 {
		return errors.New("Composer 未返回有效 PortalVersion")
	}
	if err := resumePortalVersion(client, baseURL, desired.ID, &version); err != nil {
		return err
	}

	governance, err = readPortalGovernance(client, baseURL)
	if err != nil {
		return err
	}
	portal = findPortal(governance.Portals, desired.ID)
	if portal == nil {
		return errors.New("发布后 Portal 聚合不存在")
	}
	for _, release := range portal.Releases {
		if release.PortalVersionID != version.ID {
			continue
		}
		if release.Status == portalapi.ActivationCurrent {
			return nil
		}
		if release.Status == portalapi.ActivationSuperseded {
			return errors.New("目标 PortalVersion 已进入历史上线记录；重新启用必须走显式 rollback")
		}
	}
	request := portalapi.PortalReleaseRequest{PortalVersionID: version.ID, ExpectedCurrentReleaseID: portal.CurrentReleaseID, Reason: "platformdev explicit release"}
	status, raw, err := portalRequest(client, baseURL, publisherToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/releases", desired.ID), request, true)
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

func selectPlatformPortalVersion(client *http.Client, baseURL string, portal *portalapi.Portal, desired frontendcompositionv1.ApplicationComposition) (portalapi.PortalVersion, error) {
	if portal == nil {
		status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, "/v1/portals", portalapi.PortalVersionRequest{PortalID: desired.ID, Configuration: portalapi.PortalConfiguration{Application: desired}}, true)
		if err != nil || status != http.StatusOK {
			return portalapi.PortalVersion{}, fmt.Errorf("create Portal status=%d body=%s: %w", status, raw, err)
		}
		var created portalapi.Portal
		if err := json.Unmarshal(raw, &created); err != nil || len(created.Versions) == 0 {
			return portalapi.PortalVersion{}, fmt.Errorf("Composer 未返回有效 Portal: %w", err)
		}
		return created.Versions[0], nil
	}
	if len(portal.Versions) == 0 {
		return portalapi.PortalVersion{}, errors.New("现有 Portal 没有配置版本")
	}
	latest := portal.Versions[0]
	if samePortalApplication(latest.Configuration.Application, desired) {
		return latest, nil
	}
	if latest.Status != portalapi.StatusPublished {
		return portalapi.PortalVersion{}, fmt.Errorf("Portal 已有内容不同的未完成版本 #%d (%s)，拒绝自动覆盖", latest.Number, latest.Status)
	}
	configuration := latest.Configuration
	configuration.Application = desired
	status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/versions", desired.ID), map[string]any{"configuration": configuration}, true)
	if err != nil || status != http.StatusOK {
		return portalapi.PortalVersion{}, fmt.Errorf("create PortalVersion status=%d body=%s: %w", status, raw, err)
	}
	var version portalapi.PortalVersion
	if err := json.Unmarshal(raw, &version); err != nil {
		return portalapi.PortalVersion{}, fmt.Errorf("Composer 未返回有效 PortalVersion: %w", err)
	}
	return version, nil
}

func resumePortalVersion(client *http.Client, baseURL, portalID string, version *portalapi.PortalVersion) error {
	steps := []struct {
		from      portalapi.Status
		to        portalapi.Status
		token     string
		operation string
	}{
		{portalapi.StatusDraft, portalapi.StatusPendingApproval, authorToken, "submit"},
		{portalapi.StatusPendingApproval, portalapi.StatusApproved, approverToken, "approve"},
		{portalapi.StatusApproved, portalapi.StatusPublished, publisherToken, "publish"},
	}
	for _, step := range steps {
		if version.Status != step.from {
			continue
		}
		path := fmt.Sprintf("/v1/portals/%s/versions/%d/%s", portalID, version.ID, step.operation)
		status, raw, err := portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("%s status=%d body=%s: %w", step.operation, status, raw, err)
		}
		if err := json.Unmarshal(raw, version); err != nil {
			return fmt.Errorf("decode %s PortalVersion: %w", step.operation, err)
		}
		if version.Status != step.to {
			return fmt.Errorf("%s 后 PortalVersion 状态错误: %s", step.operation, version.Status)
		}
	}
	if version.Status != portalapi.StatusPublished {
		return fmt.Errorf("PortalVersion 无法从状态 %s 继续发布", version.Status)
	}
	return nil
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
