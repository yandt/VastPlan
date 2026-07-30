package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
)

func TestReconcilePlatformPortalResumesPendingApproval(t *testing.T) {
	desired := reconcileTestApplication("/operations")
	fixture := newPortalReconcileFixture(desired, portalapi.StatusPendingApproval)
	server := httptest.NewServer(fixture)
	defer server.Close()

	if err := reconcilePlatformPortal(server.Client(), server.URL, desired); err != nil {
		t.Fatal(err)
	}
	if got, want := fixture.writes, []string{"approve", "publish", "release"}; !equalStrings(got, want) {
		t.Fatalf("部分生命周期应从持久状态继续: got=%v want=%v", got, want)
	}
	if fixture.portal.Versions[0].Status != portalapi.StatusPublished || fixture.portal.CurrentReleaseID == 0 {
		t.Fatalf("恢复后未发布上线: %+v", fixture.portal)
	}
}

func TestReconcilePlatformPortalIsNoopWhenDesiredVersionIsCurrent(t *testing.T) {
	desired := reconcileTestApplication("/operations")
	fixture := newPortalReconcileFixture(desired, portalapi.StatusPublished)
	fixture.portal.CurrentReleaseID = 9
	fixture.portal.Releases = []portalapi.PortalRelease{{ID: 9, PortalID: desired.ID, PortalVersionID: 7, Status: portalapi.ActivationCurrent}}
	server := httptest.NewServer(fixture)
	defer server.Close()

	if err := reconcilePlatformPortal(server.Client(), server.URL, desired); err != nil {
		t.Fatal(err)
	}
	if len(fixture.writes) != 0 {
		t.Fatalf("已收敛的显式 apply 必须幂等: %v", fixture.writes)
	}
}

func TestReconcilePlatformPortalDoesNotOverwriteDifferentDraft(t *testing.T) {
	desired := reconcileTestApplication("/operations")
	fixture := newPortalReconcileFixture(reconcileTestApplication("/other"), portalapi.StatusDraft)
	server := httptest.NewServer(fixture)
	defer server.Close()

	err := reconcilePlatformPortal(server.Client(), server.URL, desired)
	if err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("内容不同的人工草稿必须 fail-closed: %v", err)
	}
	if len(fixture.writes) != 0 {
		t.Fatalf("冲突草稿不得被修改: %v", fixture.writes)
	}
}

type portalReconcileFixture struct {
	portal portalapi.Portal
	writes []string
}

func newPortalReconcileFixture(application frontendcompositionv1.ApplicationComposition, status portalapi.Status) *portalReconcileFixture {
	now := "2026-07-30T00:00:00Z"
	version := portalapi.PortalVersion{ID: 7, Number: 1, TenantID: "local", PortalID: application.ID, Status: status, Configuration: portalapi.PortalConfiguration{Application: application}, CreatedAt: now, UpdatedAt: now}
	return &portalReconcileFixture{portal: portalapi.Portal{ID: application.ID, TenantID: "local", Versions: []portalapi.PortalVersion{version}, Releases: []portalapi.PortalRelease{}, CreatedAt: now, UpdatedAt: now}}
}

func (f *portalReconcileFixture) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	response.Header().Set("Content-Type", "application/json")
	if request.URL.Path == "/v1/csrf" {
		_ = json.NewEncoder(response).Encode(map[string]string{"token": "csrf"})
		return
	}
	if request.Method == http.MethodGet && request.URL.Path == "/v1/portals" {
		_ = json.NewEncoder(response).Encode(portalapi.PortalGovernanceSnapshot{Portals: []portalapi.Portal{f.portal}})
		return
	}
	version := &f.portal.Versions[0]
	switch {
	case strings.HasSuffix(request.URL.Path, "/approve"):
		f.writes = append(f.writes, "approve")
		version.Status = portalapi.StatusApproved
		_ = json.NewEncoder(response).Encode(version)
	case strings.HasSuffix(request.URL.Path, "/publish"):
		f.writes = append(f.writes, "publish")
		version.Status = portalapi.StatusPublished
		_ = json.NewEncoder(response).Encode(version)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/releases"):
		f.writes = append(f.writes, "release")
		f.portal.CurrentReleaseID = 9
		release := portalapi.PortalRelease{ID: 9, PortalID: f.portal.ID, PortalVersionID: version.ID, Status: portalapi.ActivationCurrent}
		f.portal.Releases = append(f.portal.Releases, release)
		_ = json.NewEncoder(response).Encode(release)
	default:
		http.Error(response, "unexpected request", http.StatusNotFound)
	}
}

func reconcileTestApplication(route string) frontendcompositionv1.ApplicationComposition {
	return frontendcompositionv1.ApplicationComposition{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "operations"},
		Target:   compositioncommonv1.Target{Kernel: compositioncommonv1.KernelFrontend},
		Route:    route, Plugins: []frontendcompositionv1.PluginRef{}, Config: map[string]any{},
	}
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
