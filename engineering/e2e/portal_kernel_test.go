//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"slices"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
)

func TestNodePortalKernelMTLSPrincipalProjection(t *testing.T) {
	root := repoRoot(t)
	buildPortalKernel(t, root)
	addressing := startPortalAddressingFixture(t)
	observed := make(chan *contractv1.CallContext, 1)
	addressing.register(t, func(_ context.Context, target *contractv1.CallTarget, callContext *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetOperation() != "list" || string(payload) != "{}" {
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: "request.invalid", Message: "unexpected request"}}, nil, nil
		}
		select {
		case observed <- proto.Clone(callContext).(*contractv1.CallContext):
		default:
		}
		return successfulCapability([]byte("[]"))
	})

	identity := startPortalFileIdentityFixture(t)
	process := startFilePortalKernel(t, root, addressing, identity, "")
	browser := identity.login(t, process, "alice", "acme", "portal.read")
	waitForNodePortalKernel(t, process, browser)
	page, err := browser.Get(process.baseURL() + "/operations")
	if err != nil {
		t.Fatalf("完成浏览器会话访问: %v\n%s", err, process.logs.String())
	}
	pageBody := readPortalResponse(t, page)
	if page.StatusCode != http.StatusOK || page.Request.URL.Path != "/operations" || page.Header.Get("Strict-Transport-Security") != "max-age=31536000" ||
		bytes.Contains(pageBody, []byte("__VASTPLAN_CSP_NONCE__")) || !bytes.Contains(pageBody, []byte(`id="vastplan-portal"`)) {
		t.Fatalf("登录后的 Portal 页面无效: status=%d url=%s headers=%v body=%s", page.StatusCode, page.Request.URL, page.Header, pageBody)
	}
	sessionResponse, err := browser.Get(process.baseURL() + "/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	session := decodePortalSession(t, sessionResponse)
	if sessionResponse.StatusCode != http.StatusOK || session["subject"] != "alice" || session["tenantId"] != "acme" {
		t.Fatalf("BFF Session 无效: status=%d body=%v", sessionResponse.StatusCode, session)
	}
	drafts, err := browser.Get(process.baseURL() + "/v1/portal-drafts")
	if err != nil {
		t.Fatal(err)
	}
	draftsBody := readPortalResponse(t, drafts)
	if drafts.StatusCode != http.StatusOK || !bytes.Equal(bytes.TrimSpace(draftsBody), []byte("[]")) {
		t.Fatalf("Node Portal Kernel BFF 调用失败: status=%d body=%s", drafts.StatusCode, draftsBody)
	}
	requirePortalPrincipal(t, observed)
	for _, cookie := range browser.Jar.Cookies(page.Request.URL) {
		if cookie.Name == "access_token" || cookie.Value == "must-not-reach-browser" {
			t.Fatalf("上游 Access Token 泄露到浏览器 Cookie: %+v", cookie)
		}
	}
}

func waitForNodePortalKernel(t *testing.T, process *portalKernelProcess, client *http.Client) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		response, err := client.Get(process.baseURL() + "/healthz")
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		select {
		case <-process.exited:
			t.Fatalf("Node Portal Kernel exited before readiness: %v\n%s", process.result.get(), process.logs.String())
		default:
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("Node Portal Kernel did not become ready\n%s", process.logs.String())
}

func portalKernelBrowserClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{Jar: jar, Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}}} // #nosec G402 -- ephemeral E2E certificate.
}

func requirePortalPrincipal(t *testing.T, observed <-chan *contractv1.CallContext) {
	t.Helper()
	select {
	case callContext := <-observed:
		if callContext.GetCaller().GetId() != "alice" || callContext.GetPrincipal().GetUserId() != "alice" || callContext.GetTenantId() != "acme" ||
			callContext.GetPrincipal().GetTenantId() != "acme" || !slices.Contains(callContext.GetPrincipal().GetSystemRoles(), "portal.read") || callContext.GetScene() != "portal.bff" {
			t.Fatalf("OIDC Principal 未按统一 CallContext 投影: %+v", callContext)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("未收到 Node Portal Kernel 的 Composer 调用")
	}
}

func readPortalResponse(t *testing.T, response *http.Response) []byte {
	t.Helper()
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func decodePortalSession(t *testing.T, response *http.Response) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(readPortalResponse(t, response), &value); err != nil {
		t.Fatal(err)
	}
	return value
}
