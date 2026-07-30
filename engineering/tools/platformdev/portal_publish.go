package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func publishPortal(baseURL, applicationPath string) error {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}, Timeout: 10 * time.Second}
	spec, err := frontendcompositionv1.ParseApplicationCompositionFile(applicationPath)
	if err != nil {
		return fmt.Errorf("读取初始 Portal Application Composition: %w", err)
	}
	status, raw, err := portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portals", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("governance status=%d body=%s: %w", status, raw, err)
	}
	var governance portalapi.PortalGovernanceSnapshot
	if err := json.Unmarshal(raw, &governance); err != nil {
		return fmt.Errorf("decode governance: %w", err)
	}
	var existing *portalapi.Portal
	for index := range governance.Portals {
		if governance.Portals[index].ID == spec.ID {
			existing = &governance.Portals[index]
			break
		}
	}
	var version portalapi.PortalVersion
	if existing == nil {
		status, raw, err = portalRequest(client, baseURL, authorToken, http.MethodPost, "/v1/portals", portalapi.PortalVersionRequest{PortalID: spec.ID, Configuration: portalapi.PortalConfiguration{Application: spec}}, true)
		if err == nil && status == http.StatusOK {
			var portal portalapi.Portal
			if decodeErr := json.Unmarshal(raw, &portal); decodeErr != nil || len(portal.Versions) == 0 {
				return fmt.Errorf("Composer 未返回有效 Portal: %w", decodeErr)
			}
			version = portal.Versions[0]
		}
	} else {
		if len(existing.Versions) == 0 {
			return errors.New("现有 Portal 没有可复制的配置版本")
		}
		configuration := existing.Versions[0].Configuration
		configuration.Application = spec
		path := fmt.Sprintf("/v1/portals/%s/versions", spec.ID)
		status, raw, err = portalRequest(client, baseURL, authorToken, http.MethodPost, path, map[string]any{"configuration": configuration}, true)
		if err == nil && status == http.StatusOK {
			if decodeErr := json.Unmarshal(raw, &version); decodeErr != nil {
				return fmt.Errorf("Composer 未返回有效 PortalVersion: %w", decodeErr)
			}
		}
	}
	if err != nil || status != http.StatusOK || version.ID == 0 {
		return fmt.Errorf("create PortalVersion status=%d body=%s: %w", status, raw, err)
	}
	steps := []struct{ token, operation string }{{authorToken, "submit"}, {approverToken, "approve"}, {publisherToken, "publish"}}
	for _, step := range steps {
		path := fmt.Sprintf("/v1/portals/%s/versions/%d/%s", spec.ID, version.ID, step.operation)
		status, raw, err = portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("%s status=%d body=%s: %w", step.operation, status, raw, err)
		}
	}
	// Publishing freezes a complete PortalVersion; only a separate release
	// changes the live Portal.
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portals", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("governance status=%d body=%s: %w", status, raw, err)
	}
	governance = portalapi.PortalGovernanceSnapshot{}
	if err := json.Unmarshal(raw, &governance); err != nil {
		return fmt.Errorf("decode governance: %w", err)
	}
	var expectedCurrentID uint64
	for _, portal := range governance.Portals {
		if portal.ID == spec.ID {
			expectedCurrentID = portal.CurrentReleaseID
			break
		}
	}
	releaseRequest := portalapi.PortalReleaseRequest{PortalVersionID: version.ID, ExpectedCurrentReleaseID: expectedCurrentID, Reason: "platformdev explicit release"}
	status, raw, err = portalRequest(client, baseURL, publisherToken, http.MethodPost, fmt.Sprintf("/v1/portals/%s/releases", spec.ID), releaseRequest, true)
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
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("runtime status=%d body=%s: %w", status, raw, err)
	}
	return verifyPortalColdStart(client, baseURL, devAdminToken)
}

func portalRequest(client *http.Client, baseURL, session, method, path string, payload any, csrf bool) (int, []byte, error) {
	csrfToken := ""
	if csrf {
		request, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/csrf", nil)
		request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return response.StatusCode, nil, errors.New("csrf rejected")
		}
		var result struct {
			Token string `json:"token"`
		}
		err = json.NewDecoder(response.Body).Decode(&result)
		_ = response.Body.Close()
		if err != nil || result.Token == "" {
			return 0, nil, errors.New("invalid csrf response")
		}
		csrfToken = result.Token
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	if csrfToken != "" {
		request.AddCookie(&http.Cookie{Name: "vastplan_csrf", Value: csrfToken})
		request.Header.Set("X-VastPlan-CSRF", csrfToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return response.StatusCode, raw, err
}
