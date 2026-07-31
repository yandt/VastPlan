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

func publishPortal(baseURL, applicationPath, platformCatalogPath string) error {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}, Timeout: 10 * time.Second}
	application, err := frontendcompositionv1.ParseApplicationCompositionFile(applicationPath)
	if err != nil {
		return fmt.Errorf("读取初始 Portal Application Composition: %w", err)
	}
	catalog, err := frontendcompositionv1.ParsePortalPlatformCatalogFile(platformCatalogPath)
	if err != nil {
		return fmt.Errorf("读取初始 Portal Platform Catalog: %w", err)
	}
	profile, binding, err := catalog.Resolve("local", application.ID)
	if err != nil {
		return fmt.Errorf("解析初始 Portal 平台绑定: %w", err)
	}
	desired := portalapi.PortalConfiguration{Platform: profile, Application: application, Services: binding.Services}
	if err := reconcilePlatformPortal(client, baseURL, desired); err != nil {
		return err
	}
	status, raw, err := portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if err != nil {
		return fmt.Errorf("读取 Portal Runtime: %w", err)
	}
	if status != http.StatusOK {
		return fmt.Errorf("runtime status=%d body=%s", status, raw)
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
