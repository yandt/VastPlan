package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

type fixedAssessmentCatalog struct{ page catalog.Page }

func (f fixedAssessmentCatalog) Query(catalog.Query) catalog.Page { return f.page }

type captureStatusAppender struct{ called bool }

func (a *captureStatusAppender) AppendSecurityStatus(_ pluginv1.ArtifactRef, raw []byte, _ time.Time) (*artifactassessment.StatusRecord, string, error) {
	a.called = true
	record, digest, err := artifactassessment.InspectStatus(raw)
	return &record, digest, err
}

func TestAssessmentLeaseIsExactProviderOnlyAndSingleUse(t *testing.T) {
	now := time.Date(2026, 7, 24, 5, 0, 0, 0, time.UTC)
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.2.3", Channel: "testing"}
	request := artifactassessment.ScanLeaseRequest{Ref: ref, SubjectSHA256: strings.Repeat("a", 64), SBOMSHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(request)
	store := newDataPlaneTicketStore("repo-1")
	store.now = func() time.Time { return now }
	page := catalog.Page{Total: 1, Items: []catalog.Entry{{
		Ref: ref, SHA256: request.SubjectSHA256, LifecycleStatus: catalog.LifecycleActive,
		SBOM: &platformadminapi.ArtifactSBOMDeclaration{SHA256: request.SBOMSHA256},
	}}}
	issuer, err := newAssessmentLeaseIssuer(fixedAssessmentCatalog{page: page}, store, &dataPlaneLeaseConfig{ExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Endpoint: "https://repo.example", TLSIdentity: "spiffe://vastplan/repository/repo-1"})
	if err != nil {
		t.Fatal(err)
	}
	issuer.now = func() time.Time { return now }
	issuer.random = func(output []byte) (int, error) {
		for i := range output {
			output[i] = byte(i + 1)
		}
		return len(output), nil
	}
	callCtx := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: artifactassessment.AssessmentProviderPluginID}}
	lease, err := issuer.issue(callCtx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := artifactassessment.ValidateScanLease(lease, artifactassessment.AssessmentProviderPluginID, now); err != nil {
		t.Fatal(err)
	}
	requestHTTP := httptest.NewRequest(http.MethodGet, lease.URL, nil)
	if !store.consume(requestHTTP) || requestHTTP.URL.RawQuery != "" {
		t.Fatal("扫描租约应精确消费一次并移除 secret query")
	}
	requestHTTP = httptest.NewRequest(http.MethodGet, lease.URL, nil)
	if store.consume(requestHTTP) {
		t.Fatal("扫描租约不得重复消费")
	}
	callCtx.Caller.Id = "cn.vastplan.platform.other"
	if _, err := issuer.issue(callCtx, raw); err == nil {
		t.Fatal("其他首方插件也不得取得扫描租约")
	}
}

func TestAssessmentLeaseRejectsDigestOrLifecycleMismatch(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.2.3", Channel: "testing"}
	request := artifactassessment.ScanLeaseRequest{Ref: ref, SubjectSHA256: strings.Repeat("a", 64), SBOMSHA256: strings.Repeat("b", 64)}
	raw, _ := json.Marshal(request)
	store := newDataPlaneTicketStore("repo-1")
	page := catalog.Page{Total: 1, Items: []catalog.Entry{{
		Ref: ref, SHA256: strings.Repeat("c", 64), LifecycleStatus: catalog.LifecycleYanked,
		SBOM: &platformadminapi.ArtifactSBOMDeclaration{SHA256: request.SBOMSHA256},
	}}}
	issuer, _ := newAssessmentLeaseIssuer(fixedAssessmentCatalog{page: page}, store, &dataPlaneLeaseConfig{ExposureID: "dpx_aaaaaaaaaaaaaaaaaaaa", InstanceID: "repo-1", Endpoint: "https://repo.example", TLSIdentity: "spiffe://vastplan/repository/repo-1"})
	callCtx := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: artifactassessment.AssessmentProviderPluginID}}
	if _, err := issuer.issue(callCtx, raw); err == nil {
		t.Fatal("非 active 或摘要失配制品不得签发扫描租约")
	}
}

func TestAppendAssessmentStatusRequiresExactControllerIdentity(t *testing.T) {
	_, key, _ := ed25519.GenerateKey(rand.Reader)
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.product.demo", Version: "1.2.3", Channel: "testing"}
	record, _ := artifactassessment.SignStatus(artifactassessment.StatusRecord{AdmissionSHA256: strings.Repeat("c", 64), Sequence: 1, PreviousSHA256: strings.Repeat("c", 64), Evaluation: artifactassessment.Evaluation{SubjectSHA256: strings.Repeat("a", 64), SBOMSHA256: strings.Repeat("b", 64), Scanner: artifactassessment.Scanner{ID: "trivy.filesystem", Version: "1", DatabaseRevision: strings.Repeat("d", 64)}, Decision: artifactassessment.DecisionPass, EvaluatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)}, ProviderID: "security.vastplan", KeyID: "release", PolicyID: "testing-default"}, key)
	recordRaw, _ := json.Marshal(record)
	requestRaw, _ := json.Marshal(artifactassessment.AppendStatusRequest{Ref: ref, Record: recordRaw})
	appender := &captureStatusAppender{}
	call := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: artifactassessment.AssessmentControllerPluginID}}
	if _, err := appendAssessmentStatus(appender, call, requestRaw, time.Now().UTC()); err != nil || !appender.called {
		t.Fatalf("精确 Controller 追加失败: %v", err)
	}
	appender.called = false
	call.Caller.Id = "cn.vastplan.platform.other"
	if _, err := appendAssessmentStatus(appender, call, requestRaw, time.Now().UTC()); err == nil || appender.called {
		t.Fatal("其他首方插件不得追加复扫状态")
	}
}
