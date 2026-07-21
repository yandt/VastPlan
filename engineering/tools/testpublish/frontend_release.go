package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func submitFrontendTestRelease(ctx context.Context, status developmentStatus, opts options, artifact pluginservice.Artifact, repositoryRevision uint64) error {
	origin, _, err := developmentPortal(status.Portal)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: opts.Timeout}
	bindingsPath := "/v1/portal-governance/test-target-bindings"
	var bindings []portalapi.TestTargetBinding
	if err := developmentPortalRequest(ctx, client, origin, developmentAdminSession, http.MethodGet, bindingsPath, nil, false, &bindings); err != nil {
		return fmt.Errorf("读取 Frontend TestTargetBinding: %w", err)
	}
	bindingID, err := selectOrEnsureFrontendBinding(ctx, client, origin, bindingsPath, opts, artifact.PluginID, bindings)
	if err != nil {
		return err
	}
	request := portalapi.CreateTestReleaseRequest{
		BindingID: bindingID,
		Artifact:  pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel},
		SHA256:    artifact.SHA256, RepositoryRevision: repositoryRevision,
	}
	var release portalapi.TestRelease
	if err := developmentPortalRequest(ctx, client, origin, developmentPublisherSession, http.MethodPost, "/v1/portal-governance/test-releases", request, true, &release); err != nil {
		return fmt.Errorf("提交 Frontend Test Release: %w", err)
	}
	switch release.Status {
	case portalapi.TestReleaseReady:
		fmt.Printf("Frontend 测试发布已就绪 binding=%s release=%d applicationRevision=%d activation=%d\n", bindingID, release.ID, release.CandidateApplicationRevisionID, release.CandidateActivationID)
		return nil
	case portalapi.TestReleaseRolledBack:
		return fmt.Errorf("Frontend 测试候选失败且已保留或恢复上一 Activation: release=%d code=%s message=%s", release.ID, release.ErrorCode, release.ErrorMessage)
	case portalapi.TestReleaseFailed:
		return fmt.Errorf("Frontend 测试发布失败: release=%d rollbackRequired=%t code=%s message=%s", release.ID, release.RollbackRequired, release.ErrorCode, release.ErrorMessage)
	default:
		return fmt.Errorf("Frontend Test Release 返回非终态 %s: release=%d", release.Status, release.ID)
	}
}

func selectOrEnsureFrontendBinding(ctx context.Context, client *http.Client, origin, path string, opts options, pluginID string, bindings []portalapi.TestTargetBinding) (string, error) {
	scope := portalapi.TestTargetScope(strings.TrimSpace(opts.FrontendScope))
	if scope == "" {
		scope = portalapi.TestTargetApplicationPlugin
	}
	if scope != portalapi.TestTargetApplicationPlugin && scope != portalapi.TestTargetPlatformProfilePlugin {
		return "", errors.New("-frontend-scope 只能是 application-plugin 或 platform-profile-plugin")
	}
	wantedID := strings.TrimSpace(opts.FrontendBinding)
	if wantedID != "" && !backendTargetSegment.MatchString(wantedID) {
		return "", errors.New("-frontend-binding 不是有效资源 ID")
	}
	portalID := strings.TrimSpace(opts.FrontendTarget)
	if portalID == "" {
		if wantedID == "" {
			return "", errors.New("Frontend 测试发布必须提供 -frontend-target 或 -frontend-binding")
		}
		for _, binding := range bindings {
			if binding.ID == wantedID && binding.Scope == scope && binding.PluginID == pluginID && binding.Enabled && containsString(binding.AllowedPublishers, "vastplan") {
				return wantedID, nil
			}
		}
		return "", errors.New("指定的 Frontend TestTargetBinding 不存在、未启用或与制品不匹配")
	}
	if !backendTargetSegment.MatchString(portalID) {
		return "", errors.New("-frontend-target 必须是有效 portal ID")
	}
	var exact *portalapi.TestTargetBinding
	for i := range bindings {
		binding := &bindings[i]
		if binding.Scope == scope && binding.PortalID == portalID && binding.PluginID == pluginID {
			if exact != nil && exact.ID != binding.ID {
				return "", errors.New("同一 Frontend 测试目标存在多个绑定")
			}
			exact = binding
		}
		if wantedID != "" && binding.ID == wantedID && (binding.PortalID != portalID || binding.PluginID != pluginID) {
			return "", errors.New("-frontend-binding 已被其他测试目标占用")
		}
	}
	if exact != nil {
		if wantedID != "" && wantedID != exact.ID {
			return "", errors.New("指定 Frontend binding ID 与目标已有绑定不一致")
		}
		wantedID = exact.ID
	}
	if wantedID == "" {
		wantedID = developmentFrontendBindingID(portalID, pluginID)
	}
	if exact != nil && exact.Enabled && containsString(exact.AllowedPublishers, "vastplan") {
		return wantedID, nil
	}
	publishers := []string{"vastplan"}
	version := int64(0)
	if exact != nil {
		publishers = append([]string(nil), exact.AllowedPublishers...)
		if !containsString(publishers, "vastplan") {
			publishers = append(publishers, "vastplan")
		}
		sort.Strings(publishers)
		version = exact.Version
	}
	request := portalapi.PutTestTargetBindingRequest{
		Scope: scope, PortalID: portalID, PluginID: pluginID,
		AllowedPublishers: publishers, Enabled: true, IfVersion: &version,
	}
	var saved portalapi.TestTargetBinding
	if err := developmentPortalRequest(ctx, client, origin, developmentAdminSession, http.MethodPut, path+"/"+url.PathEscape(wantedID), request, true, &saved); err != nil {
		return "", fmt.Errorf("保存 Frontend TestTargetBinding: %w", err)
	}
	if saved.ID != wantedID || saved.PortalID != portalID || saved.PluginID != pluginID || !saved.Enabled {
		return "", errors.New("Frontend TestTargetBinding 响应与请求不一致")
	}
	return wantedID, nil
}

func developmentFrontendBindingID(portalID, pluginID string) string {
	digest := sha256.Sum256([]byte(portalID + "\x00" + pluginID))
	return "dev-ui-" + hex.EncodeToString(digest[:8])
}
