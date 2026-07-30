//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestNodePortalKernelGovernanceLifecycleWithRealPlugins(t *testing.T) {
	root := repoRoot(t)
	buildPortalKernel(t, root)
	addressing := startPortalAddressingFixture(t)
	composer := startPortalComposerFixture(t, root, addressing)
	identity := startPortalFileIdentityFixture(t)
	process := startFilePortalKernel(t, root, addressing, identity, composer.deliveryOrigin)
	probe := portalKernelBrowserClient(t)
	waitForNodePortalKernel(t, process, probe)

	author := loginPortalUser(t, process, identity, "author", "portal.compose", "portal.approve")
	approver := loginPortalUser(t, process, identity, "approver", "portal.approve")
	publisher := loginPortalUser(t, process, identity, "publisher", "portal.publish")
	reader := loginPortalUser(t, process, identity, "reader", "portal.read")

	if status, _ := portalJSON(t, probe, process.baseURL(), http.MethodPost, "/v1/portals", map[string]any{}, false); status != http.StatusUnauthorized {
		t.Fatalf("匿名浏览器写请求必须拒绝: status=%d", status)
	}
	// This lifecycle fixture deliberately installs allowAllPermissions at the
	// protocol host. Role denial belongs to the real Authorization Enforcer
	// suite; duplicating it in the BFF would create a second policy source.

	firstVersion := createPublishedPortalVersion(t, process, author, approver, publisher, portalApplication(1, "Initial"))
	if status, _ := portalJSON(t, reader, process.baseURL(), http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false); status != http.StatusNotFound {
		t.Fatalf("Published PortalVersion 在上线前不得生效: status=%d", status)
	}
	firstRelease := releasePortalVersion(t, process, publisher, "operations", portalapi.PortalReleaseRequest{
		PortalVersionID: firstVersion.ID, ExpectedCurrentReleaseID: 0, Reason: "Node Portal Kernel E2E initial release",
	})
	assertPortalRuntime(t, process, reader, firstRelease.ID)

	secondVersion := createPublishedPortalVersion(t, process, author, approver, publisher, portalApplication(2, "Changed"))
	secondRelease := releasePortalVersion(t, process, publisher, "operations", portalapi.PortalReleaseRequest{
		PortalVersionID: secondVersion.ID, ExpectedCurrentReleaseID: firstRelease.ID, Reason: "Node Portal Kernel E2E second release",
	})
	assertPortalRuntime(t, process, reader, secondRelease.ID)

	status, raw := portalJSON(t, publisher, process.baseURL(), http.MethodPost,
		fmt.Sprintf("/v1/portals/operations/releases/%d/rollback", firstRelease.ID),
		map[string]any{"expectedCurrentReleaseId": secondRelease.ID, "reason": "restore first release"}, true)
	if status != http.StatusOK {
		t.Fatalf("历史 Activation 回滚失败: status=%d body=%s", status, raw)
	}
	var rollback portalapi.PortalRelease
	decodePortalJSON(t, raw, &rollback)
	if rollback.Status != portalapi.ActivationCurrent || rollback.PreviousReleaseID != secondRelease.ID || rollback.PortalVersionID != firstVersion.ID {
		t.Fatalf("回滚未基于历史 PortalVersion 创建新 Release: %+v", rollback)
	}
	assertPortalRuntime(t, process, reader, rollback.ID)
}

func loginPortalUser(t *testing.T, process *portalKernelProcess, identity *portalFileIdentityFixture, subject string, roles ...string) *http.Client {
	t.Helper()
	client := identity.login(t, process, subject, "acme", roles...)
	response, err := client.Get(process.baseURL() + "/operations")
	if err != nil {
		t.Fatalf("Portal 登录 %s: %v\n%s", subject, err, process.logs.String())
	}
	body := readPortalResponse(t, response)
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/operations" || !bytes.Contains(body, []byte(`id="vastplan-portal"`)) {
		t.Fatalf("Portal 登录 %s 未返回 Portal: status=%d url=%s body=%s", subject, response.StatusCode, response.Request.URL, body)
	}
	return client
}

func createPublishedPortalVersion(t *testing.T, process *portalKernelProcess, author, approver, publisher *http.Client, composition frontendcompositionv1.ApplicationComposition) portalapi.PortalVersion {
	t.Helper()
	governance := readGovernance(t, process, author)
	var portal *portalapi.Portal
	for index := range governance.Portals {
		if governance.Portals[index].ID == composition.ID {
			portal = &governance.Portals[index]
			break
		}
	}
	var status int
	var raw []byte
	if portal == nil {
		status, raw = portalJSON(t, author, process.baseURL(), http.MethodPost, "/v1/portals", portalapi.PortalVersionRequest{PortalID: composition.ID, Configuration: portalapi.PortalConfiguration{Application: composition}}, true)
	} else {
		configuration := portal.Versions[0].Configuration
		configuration.Application = composition
		status, raw = portalJSON(t, author, process.baseURL(), http.MethodPost, fmt.Sprintf("/v1/portals/%s/versions", composition.ID), map[string]any{"configuration": configuration}, true)
	}
	if status != http.StatusOK {
		t.Fatalf("创建 Portal 草稿失败: status=%d body=%s", status, raw)
	}
	var version portalapi.PortalVersion
	if portal == nil {
		var created portalapi.Portal
		decodePortalJSON(t, raw, &created)
		version = created.Versions[0]
	} else {
		decodePortalJSON(t, raw, &version)
	}
	if version.ID == 0 || version.Status != portalapi.StatusDraft {
		t.Fatalf("PortalVersion 草稿无效: %+v", version)
	}
	for _, transition := range []struct {
		client    *http.Client
		operation string
	}{
		{author, "submit"},
		{approver, "approve"},
		{publisher, "publish"},
	} {
		if transition.operation == "approve" {
			status, _ := portalJSON(t, author, process.baseURL(), http.MethodPost, fmt.Sprintf("/v1/portals/%s/versions/%d/approve", composition.ID, version.ID), map[string]any{}, true)
			if status != http.StatusForbidden {
				t.Fatalf("提交人不得审批自身草稿: status=%d", status)
			}
		}
		status, raw = portalJSON(t, transition.client, process.baseURL(), http.MethodPost,
			fmt.Sprintf("/v1/portals/%s/versions/%d/%s", composition.ID, version.ID, transition.operation), map[string]any{}, true)
		if status != http.StatusOK {
			t.Fatalf("Portal %s 失败: status=%d body=%s", transition.operation, status, raw)
		}
		decodePortalJSON(t, raw, &version)
	}
	if version.Status != portalapi.StatusPublished {
		t.Fatalf("PortalVersion 未发布: %+v", version)
	}
	return version
}

func readGovernance(t *testing.T, process *portalKernelProcess, client *http.Client) portalapi.PortalGovernanceSnapshot {
	t.Helper()
	status, raw := portalJSON(t, client, process.baseURL(), http.MethodGet, "/v1/portals", nil, false)
	if status != http.StatusOK {
		t.Fatalf("读取 Portal Governance 失败: status=%d body=%s", status, raw)
	}
	var snapshot portalapi.PortalGovernanceSnapshot
	decodePortalJSON(t, raw, &snapshot)
	return snapshot
}

func releasePortalVersion(t *testing.T, process *portalKernelProcess, publisher *http.Client, portalID string, request portalapi.PortalReleaseRequest) portalapi.PortalRelease {
	t.Helper()
	status, raw := portalJSON(t, publisher, process.baseURL(), http.MethodPost, fmt.Sprintf("/v1/portals/%s/releases", portalID), request, true)
	if status != http.StatusOK {
		t.Fatalf("激活 Portal 失败: status=%d body=%s", status, raw)
	}
	var release portalapi.PortalRelease
	decodePortalJSON(t, raw, &release)
	if release.Status != portalapi.ActivationCurrent || release.Resolved.Revision != release.ID {
		t.Fatalf("Portal Release 无效: %+v", release)
	}
	return release
}

func assertPortalRuntime(t *testing.T, process *portalKernelProcess, reader *http.Client, activationRevision uint64) {
	t.Helper()
	status, raw := portalJSON(t, reader, process.baseURL(), http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if status != http.StatusOK {
		t.Fatalf("读取 Portal Runtime 失败: status=%d body=%s", status, raw)
	}
	var runtime portalapi.RuntimeSpec
	decodePortalJSON(t, raw, &runtime)
	if runtime.Portal.Revision != activationRevision || len(runtime.Modules) != len(runtime.Portal.Plugins) || len(runtime.ModuleGraphs) != 0 {
		t.Fatalf("Portal Runtime 未绑定完整 Activation: activationRevision=%d runtime=%+v", activationRevision, runtime)
	}
	module, err := reader.Get(process.baseURL() + runtime.Modules[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	moduleBody := readPortalResponse(t, module)
	if module.StatusCode != http.StatusOK || len(moduleBody) == 0 {
		t.Fatalf("内容寻址模块不可读: status=%d body=%s", module.StatusCode, moduleBody)
	}
}

func portalJSON(t *testing.T, client *http.Client, baseURL, method, path string, payload any, csrf bool) (int, []byte) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		t.Fatal(err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if csrf {
		token := portalKernelCSRF(t, client, baseURL)
		request.Header.Set("X-VastPlan-CSRF", token)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, readPortalResponse(t, response)
}

func portalKernelCSRF(t *testing.T, client *http.Client, baseURL string) string {
	t.Helper()
	response, err := client.Get(baseURL + "/v1/csrf")
	if err != nil {
		t.Fatal(err)
	}
	var value struct {
		Token string `json:"token"`
	}
	decodePortalJSON(t, readPortalResponse(t, response), &value)
	if response.StatusCode != http.StatusOK || value.Token == "" {
		t.Fatalf("取得 CSRF 失败: status=%d", response.StatusCode)
	}
	return value.Token
}

func decodePortalJSON(t *testing.T, raw []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(raw, target); err != nil {
		t.Fatalf("解析 Portal JSON: %v body=%s", err, raw)
	}
}

func portalApplication(revision uint64, title string) frontendcompositionv1.ApplicationComposition {
	return frontendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: revision, ID: "operations"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend}, Route: "/operations",
		Branding: map[string]any{"title": title}, Plugins: []frontendcompositionv1.PluginRef{},
	}
}
