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
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func TestNodePortalKernelGovernanceLifecycleWithRealPlugins(t *testing.T) {
	root := repoRoot(t)
	buildPortalKernel(t, root)
	addressing := startPortalAddressingFixture(t)
	composer := startPortalComposerFixture(t, root, addressing)
	oidc := startPortalOIDCProvider(t, "vastplan-portal-governance-e2e")
	process := startOIDCPortalKernel(t, root, addressing, oidc, composer.deliveryOrigin)
	probe := portalKernelBrowserClient(t)
	waitForNodePortalKernel(t, process, probe)

	author := loginPortalUser(t, process, oidc, "author", "portal.compose", "portal.approve")
	approver := loginPortalUser(t, process, oidc, "approver", "portal.approve")
	publisher := loginPortalUser(t, process, oidc, "publisher", "portal.publish")
	reader := loginPortalUser(t, process, oidc, "reader", "portal.read")

	if status, _ := portalJSON(t, probe, process.baseURL(), http.MethodPost, "/v1/portal-drafts", portalApplication(1, "Initial"), false); status != http.StatusUnauthorized {
		t.Fatalf("匿名浏览器写请求必须拒绝: status=%d", status)
	}
	if status, _ := portalJSON(t, reader, process.baseURL(), http.MethodPost, "/v1/portal-drafts", portalApplication(1, "Initial"), true); status != http.StatusForbidden {
		t.Fatalf("只读身份不能创建草稿: status=%d", status)
	}

	firstRevision := createPublishedPortalRevision(t, process, author, approver, publisher, portalApplication(1, "Initial"))
	if status, _ := portalJSON(t, reader, process.baseURL(), http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false); status != http.StatusNotFound {
		t.Fatalf("Published Application 在 Activation 前不得上线: status=%d", status)
	}
	governance := readGovernance(t, process, reader)
	profile, binding := publishedPortalInputs(t, governance, "operations")
	firstActivation := activatePortalRevision(t, process, publisher, portalapi.ActivationRequest{
		PortalID: "operations", ApplicationRevisionID: firstRevision.ID, ProfileRevisionID: profile.ID, BindingRevisionID: binding.ID,
		ExpectedCurrentID: 0, Reason: "Node Portal Kernel E2E initial activation",
	})
	assertPortalRuntime(t, process, reader, firstActivation.ID)

	secondRevision := createPublishedPortalRevision(t, process, author, approver, publisher, portalApplication(2, "Changed"))
	secondActivation := activatePortalRevision(t, process, publisher, portalapi.ActivationRequest{
		PortalID: "operations", ApplicationRevisionID: secondRevision.ID, ProfileRevisionID: profile.ID, BindingRevisionID: binding.ID,
		ExpectedCurrentID: firstActivation.ID, Reason: "Node Portal Kernel E2E second activation",
	})
	assertPortalRuntime(t, process, reader, secondActivation.ID)

	status, raw := portalJSON(t, publisher, process.baseURL(), http.MethodPost,
		fmt.Sprintf("/v1/portal-governance/activations/%d/rollback", firstActivation.ID),
		map[string]any{"expectedCurrentId": secondActivation.ID, "reason": "restore first activation"}, true)
	if status != http.StatusOK {
		t.Fatalf("历史 Activation 回滚失败: status=%d body=%s", status, raw)
	}
	var rollback portalapi.PortalActivation
	decodePortalJSON(t, raw, &rollback)
	if rollback.Status != portalapi.ActivationCurrent || rollback.PreviousActivationID != secondActivation.ID || rollback.ApplicationRevisionID != firstRevision.ID {
		t.Fatalf("回滚未创建绑定历史输入的新 Activation: %+v", rollback)
	}
	assertPortalRuntime(t, process, reader, rollback.ID)
}

func loginPortalUser(t *testing.T, process *portalKernelProcess, provider *portalOIDCProvider, subject string, roles ...string) *http.Client {
	t.Helper()
	provider.selectIdentity(subject, "acme", roles...)
	client := portalKernelBrowserClient(t)
	response, err := client.Get(process.baseURL() + "/operations")
	if err != nil {
		t.Fatalf("OIDC 登录 %s: %v\n%s", subject, err, process.logs.String())
	}
	body := readPortalResponse(t, response)
	if response.StatusCode != http.StatusOK || response.Request.URL.Path != "/operations" || !bytes.Contains(body, []byte(`id="vastplan-portal"`)) {
		t.Fatalf("OIDC 登录 %s 未返回 Portal: status=%d url=%s body=%s", subject, response.StatusCode, response.Request.URL, body)
	}
	return client
}

func createPublishedPortalRevision(t *testing.T, process *portalKernelProcess, author, approver, publisher *http.Client, composition frontendcompositionv1.ApplicationComposition) portalapi.Revision {
	t.Helper()
	status, raw := portalJSON(t, author, process.baseURL(), http.MethodPost, "/v1/portal-drafts", composition, true)
	if status != http.StatusOK {
		t.Fatalf("创建 Portal 草稿失败: status=%d body=%s", status, raw)
	}
	var revision portalapi.Revision
	decodePortalJSON(t, raw, &revision)
	if revision.ID == 0 || revision.Status != portalapi.StatusDraft {
		t.Fatalf("草稿 revision 无效: %+v", revision)
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
			status, _ := portalJSON(t, author, process.baseURL(), http.MethodPost, fmt.Sprintf("/v1/portal-drafts/%d/approve", revision.ID), map[string]any{}, true)
			if status != http.StatusForbidden {
				t.Fatalf("提交人不得审批自身草稿: status=%d", status)
			}
		}
		status, raw = portalJSON(t, transition.client, process.baseURL(), http.MethodPost,
			fmt.Sprintf("/v1/portal-drafts/%d/%s", revision.ID, transition.operation), map[string]any{}, true)
		if status != http.StatusOK {
			t.Fatalf("Portal %s 失败: status=%d body=%s", transition.operation, status, raw)
		}
		decodePortalJSON(t, raw, &revision)
	}
	if revision.Status != portalapi.StatusPublished {
		t.Fatalf("Portal revision 未发布: %+v", revision)
	}
	return revision
}

func readGovernance(t *testing.T, process *portalKernelProcess, client *http.Client) portalapi.GovernanceSnapshot {
	t.Helper()
	status, raw := portalJSON(t, client, process.baseURL(), http.MethodGet, "/v1/portal-governance", nil, false)
	if status != http.StatusOK {
		t.Fatalf("读取 Portal Governance 失败: status=%d body=%s", status, raw)
	}
	var snapshot portalapi.GovernanceSnapshot
	decodePortalJSON(t, raw, &snapshot)
	return snapshot
}

func publishedPortalInputs(t *testing.T, snapshot portalapi.GovernanceSnapshot, portalID string) (portalapi.PlatformProfileRevision, portalapi.BindingRevision) {
	t.Helper()
	for _, binding := range snapshot.Bindings {
		if binding.PortalID != portalID || binding.Status != portalapi.StatusPublished {
			continue
		}
		for _, profile := range snapshot.Profiles {
			if profile.ID == binding.ProfileRevisionID && profile.Status == portalapi.StatusPublished {
				return profile, binding
			}
		}
	}
	t.Fatalf("Portal %q 缺少 Published Profile/Binding: %+v", portalID, snapshot)
	return portalapi.PlatformProfileRevision{}, portalapi.BindingRevision{}
}

func activatePortalRevision(t *testing.T, process *portalKernelProcess, publisher *http.Client, request portalapi.ActivationRequest) portalapi.PortalActivation {
	t.Helper()
	status, raw := portalJSON(t, publisher, process.baseURL(), http.MethodPost, "/v1/portal-governance/activations", request, true)
	if status != http.StatusOK {
		t.Fatalf("激活 Portal 失败: status=%d body=%s", status, raw)
	}
	var activation portalapi.PortalActivation
	decodePortalJSON(t, raw, &activation)
	if activation.Status != portalapi.ActivationCurrent || activation.Spec.Revision != activation.ID {
		t.Fatalf("Portal Activation 无效: %+v", activation)
	}
	return activation
}

func assertPortalRuntime(t *testing.T, process *portalKernelProcess, reader *http.Client, revision uint64) {
	t.Helper()
	status, raw := portalJSON(t, reader, process.baseURL(), http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if status != http.StatusOK {
		t.Fatalf("读取 Portal Runtime 失败: status=%d body=%s", status, raw)
	}
	var runtime portalapi.RuntimeSpec
	decodePortalJSON(t, raw, &runtime)
	if runtime.Portal.Revision != revision || len(runtime.Modules) != 14 || len(runtime.ModuleGraphs) != 0 {
		t.Fatalf("Portal Runtime 未绑定完整 Activation: revision=%d runtime=%+v", revision, runtime)
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
