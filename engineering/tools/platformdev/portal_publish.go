package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func publishPortal(baseURL string) error {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}, Timeout: 10 * time.Second}
	spec := map[string]any{
		"version": 1, "revision": 1, "id": "operations", "target": map[string]string{"kernel": "frontend"},
		"route": "/operations", "audience": []string{"portal.read"}, "plugins": []any{
			map[string]any{"id": "cn.vastplan.product.developer.workbench-gallery", "version": "0.1.0", "channel": "stable"},
		}, "config": map[string]any{},
		"branding": map[string]any{"title": "VastPlan 平台管理中心"},
	}
	status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, "/v1/portal-drafts", spec, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("create status=%d body=%s: %w", status, raw, err)
	}
	var revision struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &revision); err != nil || revision.ID == 0 {
		return errors.New("Composer 未返回有效 revision")
	}
	steps := []struct{ token, operation string }{{authorToken, "submit"}, {approverToken, "approve"}, {publisherToken, "publish"}}
	for _, step := range steps {
		path := fmt.Sprintf("/v1/portal-drafts/%d/%s", revision.ID, step.operation)
		status, raw, err = portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("%s status=%d body=%s: %w", step.operation, status, raw, err)
		}
	}
	// Published Application/Profile/Binding revisions are eligible inputs only.
	// Select the catalog-seeded Profile + Binding and make the initial live fact
	// explicit through the same CAS-protected Activation API used in production.
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-governance", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("governance status=%d body=%s: %w", status, raw, err)
	}
	var governance portalapi.GovernanceSnapshot
	if err := json.Unmarshal(raw, &governance); err != nil {
		return fmt.Errorf("decode governance: %w", err)
	}
	var binding portalapi.BindingRevision
	for _, candidate := range governance.Bindings {
		if candidate.PortalID == "operations" && candidate.Status == portalapi.StatusPublished && candidate.ID > binding.ID {
			binding = candidate
		}
	}
	if binding.ID == 0 {
		return errors.New("未找到 operations 的已发布 Portal Binding")
	}
	var profile portalapi.PlatformProfileRevision
	for _, candidate := range governance.Profiles {
		if candidate.ID == binding.ProfileRevisionID && candidate.Status == portalapi.StatusPublished {
			profile = candidate
			break
		}
	}
	if profile.ID == 0 {
		return fmt.Errorf("Binding #%d 引用的 Profile #%d 不可用", binding.ID, binding.ProfileRevisionID)
	}
	var expectedCurrentID uint64
	for _, candidate := range governance.Activations {
		if candidate.PortalID == "operations" && candidate.Status == portalapi.ActivationCurrent {
			expectedCurrentID = candidate.ID
			break
		}
	}
	activationRequest := portalapi.ActivationRequest{
		PortalID: "operations", ApplicationRevisionID: revision.ID, ProfileRevisionID: profile.ID,
		BindingRevisionID: binding.ID, ExpectedCurrentID: expectedCurrentID, Reason: "platformdev startup activation",
	}
	status, raw, err = portalRequest(client, baseURL, publisherToken, http.MethodPost, "/v1/portal-governance/activations", activationRequest, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("activate status=%d body=%s: %w", status, raw, err)
	}
	var activation portalapi.PortalActivation
	if err := json.Unmarshal(raw, &activation); err != nil {
		return fmt.Errorf("decode activation: %w", err)
	}
	if activation.Status != portalapi.ActivationCurrent {
		return fmt.Errorf("initial Portal activation failed: %+v", activation)
	}
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("runtime status=%d body=%s: %w", status, raw, err)
	}
	return verifyPortalSSR(client, baseURL, devAdminToken)
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
