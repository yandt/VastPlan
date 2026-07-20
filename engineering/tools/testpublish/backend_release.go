package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

const (
	developmentAdminSession     = "vastplan-local-platform-admin"
	developmentPublisherSession = "vastplan-local-portal-publisher"
)

var backendTargetSegment = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func submitBackendTestRelease(ctx context.Context, status developmentStatus, opts options, artifact pluginservice.Artifact, repositoryRevision uint64) error {
	portalOrigin, portalID, err := developmentPortal(status.Portal)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: opts.Timeout}
	basePath := "/v1/portals/" + url.PathEscape(portalID) + "/platform/services/deployment/deployment"
	bindingsPath := basePath + "/test-target-bindings"
	bindings, err := readTestTargetBindings(ctx, client, portalOrigin, bindingsPath)
	if err != nil {
		return err
	}
	bindingID, err := selectOrEnsureBinding(ctx, client, portalOrigin, bindingsPath, opts, artifact.PluginID, bindings)
	if err != nil {
		return err
	}
	request := platformadminapi.CreateTestReleaseRequest{
		BindingID: bindingID,
		Artifact: pluginv1.ArtifactRef{
			PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel,
		},
		SHA256: artifact.SHA256, RepositoryRevision: repositoryRevision,
	}
	var release platformadminapi.TestRelease
	if err := developmentPortalRequest(ctx, client, portalOrigin, developmentPublisherSession, http.MethodPost, basePath+"/test-releases", request, true, &release); err != nil {
		return fmt.Errorf("提交 Backend Test Release: %w", err)
	}
	switch release.Status {
	case platformadminapi.TestReleaseReady:
		fmt.Printf("Backend 测试发布已就绪 binding=%s release=%d serviceRevision=%d\n", bindingID, release.ID, release.CandidateServiceRevisionID)
		return nil
	case platformadminapi.TestReleaseRolledBack:
		return fmt.Errorf("Backend 测试候选失败且已安全回滚: release=%d code=%s message=%s", release.ID, release.ErrorCode, release.ErrorMessage)
	case platformadminapi.TestReleaseFailed:
		return fmt.Errorf("Backend 测试发布失败: release=%d rollbackRequired=%t code=%s message=%s", release.ID, release.RollbackRequired, release.ErrorCode, release.ErrorMessage)
	default:
		return fmt.Errorf("Backend Test Release 返回非终态 %s: release=%d", release.Status, release.ID)
	}
}

func readTestTargetBindings(ctx context.Context, client *http.Client, origin, path string) ([]platformadminapi.TestTargetBinding, error) {
	var bindings []platformadminapi.TestTargetBinding
	if err := developmentPortalRequest(ctx, client, origin, developmentAdminSession, http.MethodGet, path, nil, false, &bindings); err != nil {
		return nil, fmt.Errorf("读取 Backend TestTargetBinding: %w", err)
	}
	return bindings, nil
}

func selectOrEnsureBinding(ctx context.Context, client *http.Client, origin, path string, opts options, pluginID string, bindings []platformadminapi.TestTargetBinding) (string, error) {
	wantedID := strings.TrimSpace(opts.BackendBinding)
	if wantedID != "" && !backendTargetSegment.MatchString(wantedID) {
		return "", errors.New("-backend-binding 不是有效资源 ID")
	}
	if strings.TrimSpace(opts.BackendTarget) == "" {
		if wantedID == "" {
			return "", errors.New("Backend 测试发布必须提供 -backend-target 或 -backend-binding")
		}
		for _, binding := range bindings {
			if binding.ID == wantedID && binding.Kind == platformadminapi.TestTargetBackend && binding.PluginID == pluginID && binding.Enabled && containsString(binding.AllowedPublishers, "vastplan") {
				return wantedID, nil
			}
		}
		return "", errors.New("指定的 Backend TestTargetBinding 不存在、未启用或与制品不匹配")
	}
	deployment, unit, err := parseBackendTarget(opts.BackendTarget)
	if err != nil {
		return "", err
	}
	var exact *platformadminapi.TestTargetBinding
	for i := range bindings {
		binding := &bindings[i]
		if binding.Kind == platformadminapi.TestTargetBackend && binding.Deployment == deployment && binding.UnitID == unit && binding.PluginID == pluginID {
			if exact != nil && exact.ID != binding.ID {
				return "", errors.New("同一 Backend 测试目标存在多个绑定")
			}
			exact = binding
		}
		if wantedID != "" && binding.ID == wantedID && (binding.Deployment != deployment || binding.UnitID != unit || binding.PluginID != pluginID) {
			return "", errors.New("-backend-binding 已被其他测试目标占用")
		}
	}
	if exact != nil {
		if wantedID != "" && wantedID != exact.ID {
			return "", errors.New("指定 binding ID 与目标已有绑定不一致")
		}
		wantedID = exact.ID
	}
	if wantedID == "" {
		wantedID = developmentBindingID(deployment, unit, pluginID)
	}
	if exact != nil && exact.Enabled && containsString(exact.AllowedPublishers, "vastplan") {
		return wantedID, nil
	}
	publishers := []string{"vastplan"}
	var ifVersion *int64
	if exact != nil {
		publishers = append([]string(nil), exact.AllowedPublishers...)
		if !containsString(publishers, "vastplan") {
			publishers = append(publishers, "vastplan")
		}
		sort.Strings(publishers)
		version := exact.Version
		ifVersion = &version
	} else {
		zero := int64(0)
		ifVersion = &zero
	}
	request := platformadminapi.PutTestTargetBindingRequest{
		Kind: platformadminapi.TestTargetBackend, Deployment: deployment, UnitID: unit, PluginID: pluginID,
		AllowedPublishers: publishers, Enabled: true, IfVersion: ifVersion,
	}
	var saved platformadminapi.TestTargetBinding
	if err := developmentPortalRequest(ctx, client, origin, developmentAdminSession, http.MethodPut, path+"/"+url.PathEscape(wantedID), request, true, &saved); err != nil {
		return "", fmt.Errorf("保存 Backend TestTargetBinding: %w", err)
	}
	if saved.ID != wantedID || saved.Deployment != deployment || saved.UnitID != unit || saved.PluginID != pluginID || !saved.Enabled {
		return "", errors.New("Backend TestTargetBinding 响应与请求不一致")
	}
	return wantedID, nil
}

func developmentPortal(raw string) (string, string, error) {
	parsed, err := loopbackURL(raw, false)
	if err != nil || parsed.RawQuery != "" {
		return "", "", errors.New("本地平台没有返回合法的回环 Portal URL")
	}
	parts := strings.Split(strings.Trim(parsed.EscapedPath(), "/"), "/")
	if len(parts) != 1 || parts[0] == "" {
		return "", "", errors.New("本地 Portal URL 必须精确指向一个 portal ID")
	}
	portalID, err := url.PathUnescape(parts[0])
	if err != nil || !backendTargetSegment.MatchString(portalID) {
		return "", "", errors.New("本地 Portal ID 无效")
	}
	return parsed.Scheme + "://" + parsed.Host, portalID, nil
}

func parseBackendTarget(raw string) (string, string, error) {
	parts := strings.Split(strings.TrimSpace(raw), "/")
	if len(parts) != 2 || !backendTargetSegment.MatchString(parts[0]) || !backendTargetSegment.MatchString(parts[1]) {
		return "", "", errors.New("-backend-target 必须为 deployment/unit，且两段都是有效资源 ID")
	}
	return parts[0], parts[1], nil
}

func developmentBindingID(deployment, unit, pluginID string) string {
	digest := sha256.Sum256([]byte(deployment + "\x00" + unit + "\x00" + pluginID))
	return "dev-" + hex.EncodeToString(digest[:8])
}

func developmentPortalRequest(ctx context.Context, client *http.Client, origin, session, method, path string, payload any, csrf bool, output any) error {
	csrfToken := ""
	if csrf {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+"/v1/csrf", nil)
		if err != nil {
			return err
		}
		request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		var token struct {
			Token string `json:"token"`
		}
		decodeErr := decodeDevelopmentResponse(response, &token)
		if decodeErr != nil || token.Token == "" {
			return fmt.Errorf("获取开发 Portal CSRF: %w", coalesceDevelopmentError(decodeErr, errors.New("token 为空")))
		}
		csrfToken = token.Token
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequestWithContext(ctx, method, origin+path, body)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	if csrfToken != "" {
		request.AddCookie(&http.Cookie{Name: "vastplan_csrf", Value: csrfToken})
		request.Header.Set("X-VastPlan-CSRF", csrfToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	return decodeDevelopmentResponse(response, output)
}

func decodeDevelopmentResponse(response *http.Response, output any) error {
	defer response.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var failure struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(raw, &failure)
		if failure.Error == "" {
			failure.Error = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("Portal 返回 %s: %s", response.Status, failure.Error)
	}
	if output == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(output); err != nil {
		return fmt.Errorf("解析 Portal 响应: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("Portal 响应只能包含一个 JSON 文档")
	}
	return nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func coalesceDevelopmentError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
