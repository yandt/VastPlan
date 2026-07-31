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
	desired := reconcileTestConfiguration("/operations")
	fixture := newPortalReconcileFixture(desired, portalapi.StatusPendingApproval)
	server := httptest.NewServer(fixture)
	defer server.Close()

	if err := reconcilePlatformPortal(server.Client(), server.URL, desired); err != nil {
		t.Fatal(err)
	}
	if got, want := fixture.writes, []string{"approve", "publish", "release"}; !equalStrings(got, want) {
		t.Fatalf("部分生命周期应从持久状态继续: got=%v want=%v", got, want)
	}
	if fixture.portal.PublishedPublication == nil || fixture.portal.PublishedPublication.Status != portalapi.StatusPublished || fixture.portal.CurrentReleaseID == 0 {
		t.Fatalf("恢复后未发布上线: %+v", fixture.portal)
	}
}

func TestReconcilePlatformPortalIsNoopWhenDesiredPublicationIsCurrent(t *testing.T) {
	desired := reconcileTestConfiguration("/operations")
	fixture := newPortalReconcileFixture(desired, portalapi.StatusPublished)
	fixture.portal.CurrentReleaseID = 9
	fixture.portal.Releases = []portalapi.PortalRelease{{ID: 9, PortalID: desired.Application.ID, PublicationID: 7, Status: portalapi.ActivationCurrent}}
	server := httptest.NewServer(fixture)
	defer server.Close()

	if err := reconcilePlatformPortal(server.Client(), server.URL, desired); err != nil {
		t.Fatal(err)
	}
	if len(fixture.writes) != 0 {
		t.Fatalf("已收敛的显式 apply 必须幂等: %v", fixture.writes)
	}
}

func TestReconcilePlatformPortalReleasesNewVersionWhenPlatformProfileChanges(t *testing.T) {
	current := reconcileTestConfiguration("/operations")
	current.Platform.Document = compositioncommonv1.Document{Version: 1, Revision: 1, ID: "operations.platform"}
	desired := current
	desired.Platform.Document.Revision = 2
	fixture := newPortalReconcileFixture(current, portalapi.StatusPublished)
	fixture.portal.CurrentReleaseID = 9
	fixture.portal.Releases = []portalapi.PortalRelease{{ID: 9, PortalID: desired.Application.ID, PublicationID: 7, Status: portalapi.ActivationCurrent}}
	server := httptest.NewServer(fixture)
	defer server.Close()

	if err := reconcilePlatformPortal(server.Client(), server.URL, desired); err != nil {
		t.Fatal(err)
	}
	if got, want := fixture.writes, []string{"working-copy", "submit", "approve", "publish", "release"}; !equalStrings(got, want) {
		t.Fatalf("Platform Profile 变化必须形成新的 Portal 版本和上线记录: got=%v want=%v", got, want)
	}
}

func TestReconcilePlatformPortalDoesNotOverwriteDifferentWorkingCopy(t *testing.T) {
	desired := reconcileTestConfiguration("/operations")
	fixture := newPortalReconcileFixture(reconcileTestConfiguration("/other"), portalapi.StatusDraft)
	server := httptest.NewServer(fixture)
	defer server.Close()

	err := reconcilePlatformPortal(server.Client(), server.URL, desired)
	if err == nil || !strings.Contains(err.Error(), "拒绝自动覆盖") {
		t.Fatalf("内容不同的人工 WorkingCopy 必须 fail-closed: %v", err)
	}
	if len(fixture.writes) != 0 {
		t.Fatalf("冲突 WorkingCopy 不得被修改: %v", fixture.writes)
	}
}

type portalReconcileFixture struct {
	portal portalapi.Portal
	writes []string
}

func newPortalReconcileFixture(configuration portalapi.PortalConfiguration, status portalapi.Status) *portalReconcileFixture {
	now := "2026-07-30T00:00:00Z"
	portal := portalapi.Portal{
		ID: configuration.Application.ID, TenantID: "local", Releases: []portalapi.PortalRelease{},
		VersionControl: portalapi.PortalVersionControlStatus{Availability: portalapi.PortalVersionControlDisabled, Capabilities: []string{}},
		CreatedAt:      now, UpdatedAt: now,
	}
	if status == portalapi.StatusDraft {
		portal.WorkingCopy = &portalapi.PortalWorkingCopy{TenantID: "local", PortalID: configuration.Application.ID, Revision: 1, Configuration: configuration, Digest: strings.Repeat("a", 64), CreatedAt: now, UpdatedAt: now}
	} else {
		publication := portalapi.PortalPublication{
			ID: 7, TenantID: "local", PortalID: configuration.Application.ID, WorkingRevision: 1, Status: status, Digest: strings.Repeat("a", 64),
			Source: portalapi.PortalPublicationSource{Kind: portalapi.PortalPublicationSourceInline, Configuration: &configuration}, SubmittedBy: "author", CreatedAt: now, UpdatedAt: now,
		}
		if status == portalapi.StatusPublished {
			portal.PublishedPublication = &publication
		} else {
			portal.PendingPublication = &publication
		}
	}
	return &portalReconcileFixture{portal: portal}
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
	switch {
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/working-copy"):
		var payload struct {
			Configuration portalapi.PortalConfiguration `json:"configuration"`
		}
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			http.Error(response, "invalid body", http.StatusBadRequest)
			return
		}
		f.writes = append(f.writes, "working-copy")
		f.portal.WorkingCopy = &portalapi.PortalWorkingCopy{TenantID: "local", PortalID: f.portal.ID, Revision: 2, Configuration: payload.Configuration}
		_ = json.NewEncoder(response).Encode(f.portal.WorkingCopy)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/publications"):
		f.writes = append(f.writes, "submit")
		publicationID := uint64(7)
		if f.portal.PublishedPublication != nil {
			publicationID = f.portal.PublishedPublication.ID + 1
		}
		publication := portalapi.PortalPublication{ID: publicationID, PortalID: f.portal.ID, WorkingRevision: f.portal.WorkingCopy.Revision, Status: portalapi.StatusPendingApproval, Source: portalapi.PortalPublicationSource{Kind: portalapi.PortalPublicationSourceInline, Configuration: &f.portal.WorkingCopy.Configuration}}
		f.portal.WorkingCopy, f.portal.PendingPublication = nil, &publication
		_ = json.NewEncoder(response).Encode(publication)
	case strings.HasSuffix(request.URL.Path, "/approve"):
		f.writes = append(f.writes, "approve")
		f.portal.PendingPublication.Status = portalapi.StatusApproved
		_ = json.NewEncoder(response).Encode(f.portal.PendingPublication)
	case strings.HasSuffix(request.URL.Path, "/publish"):
		f.writes = append(f.writes, "publish")
		f.portal.PendingPublication.Status = portalapi.StatusPublished
		f.portal.PublishedPublication, f.portal.PendingPublication = f.portal.PendingPublication, nil
		_ = json.NewEncoder(response).Encode(f.portal.PublishedPublication)
	case request.Method == http.MethodPost && strings.HasSuffix(request.URL.Path, "/releases"):
		f.writes = append(f.writes, "release")
		f.portal.CurrentReleaseID = 9
		release := portalapi.PortalRelease{ID: 9, PortalID: f.portal.ID, PublicationID: f.portal.PublishedPublication.ID, Status: portalapi.ActivationCurrent}
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

func reconcileTestConfiguration(route string) portalapi.PortalConfiguration {
	return portalapi.PortalConfiguration{Application: reconcileTestApplication(route)}
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
