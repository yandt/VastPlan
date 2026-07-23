package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.repository/catalog"
)

type assessmentCatalog interface {
	Query(catalog.Query) catalog.Page
}

type assessmentLeaseIssuer struct {
	catalog    assessmentCatalog
	tickets    *dataPlaneTicketStore
	endpoint   string
	exposureID string
	now        func() time.Time
	random     func([]byte) (int, error)
}

func newAssessmentLeaseIssuer(store assessmentCatalog, tickets *dataPlaneTicketStore, config *dataPlaneLeaseConfig) (*assessmentLeaseIssuer, error) {
	if store == nil || tickets == nil || config == nil {
		return nil, nil
	}
	endpoint := strings.TrimSuffix(config.Endpoint, "/")
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		return nil, errors.New("安全评估扫描租约 endpoint 无效")
	}
	return &assessmentLeaseIssuer{catalog: store, tickets: tickets, endpoint: endpoint, exposureID: config.ExposureID, now: time.Now, random: rand.Read}, nil
}

func (i *assessmentLeaseIssuer) issue(callCtx *contractv1.CallContext, raw []byte) (artifactassessment.ScanLease, error) {
	if i == nil || callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != artifactassessment.AssessmentProviderPluginID || callCtx.GetTenantId() == "" {
		return artifactassessment.ScanLease{}, errors.New("扫描租约只授权精确首方 Assessment Provider")
	}
	var request artifactassessment.ScanLeaseRequest
	if err := decodeParams(raw, &request); err != nil || artifactassessment.ValidateScanLeaseRequest(request) != nil {
		return artifactassessment.ScanLease{}, errors.New("安全评估扫描租约请求无效")
	}
	page := i.catalog.Query(catalog.Query{PluginID: request.Ref.PluginID, Version: request.Ref.Version, Channel: request.Ref.Channel, Page: 1, PageSize: 2})
	if page.Total != 1 || len(page.Items) != 1 {
		return artifactassessment.ScanLease{}, errors.New("安全评估制品不存在或精确 ref 不唯一")
	}
	entry := page.Items[0]
	if entry.Ref != request.Ref || entry.LifecycleStatus != catalog.LifecycleActive || entry.SHA256 != request.SubjectSHA256 || entry.SBOM == nil || entry.SBOM.SHA256 != request.SBOMSHA256 {
		return artifactassessment.ScanLease{}, errors.New("安全评估扫描租约未绑定 active 制品或 SBOM")
	}
	resource := fmt.Sprintf("/v1/artifacts/%s/%s/%s/package", url.PathEscape(request.Ref.PluginID), url.PathEscape(request.Ref.Version), url.PathEscape(request.Ref.Channel))
	tokenBytes := make([]byte, 32)
	if n, err := i.random(tokenBytes); err != nil || n != len(tokenBytes) {
		return artifactassessment.ScanLease{}, errors.New("生成安全评估扫描 ticket 失败")
	}
	ticket := base64.RawURLEncoding.EncodeToString(tokenBytes)
	now := i.now().UTC()
	expiresAt := now.Add(artifactassessment.ScanLeaseTTL)
	claims := apiv1.DataPlaneTicketClaims{
		TenantID: callCtx.GetTenantId(), PrincipalID: artifactassessment.AssessmentProviderPluginID,
		DataPlaneExposureID: i.exposureID, InstanceID: i.tickets.instanceID, Method: http.MethodGet, Resource: resource, ExpiresAt: expiresAt,
	}
	if err := i.tickets.installClaims(ticket, claims); err != nil {
		return artifactassessment.ScanLease{}, err
	}
	lease := artifactassessment.ScanLease{
		SchemaVersion: artifactassessment.SchemaVersion, Ref: request.Ref, SubjectSHA256: request.SubjectSHA256, SBOMSHA256: request.SBOMSHA256,
		Audience: artifactassessment.AssessmentProviderPluginID, URL: i.endpoint + resource + "?vp_ticket=" + url.QueryEscape(ticket), ExpiresAt: expiresAt,
	}
	if err := artifactassessment.ValidateScanLease(lease, artifactassessment.AssessmentProviderPluginID, now); err != nil {
		return artifactassessment.ScanLease{}, err
	}
	return lease, nil
}

func marshalScanLease(value artifactassessment.ScanLease) ([]byte, error) { return json.Marshal(value) }
