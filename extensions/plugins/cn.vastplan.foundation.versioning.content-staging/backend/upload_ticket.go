package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	stagingv1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionstaging/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
)

const apiExposurePluginID = "cn.vastplan.platform.integration.api-exposure"

var (
	uploadTicketPattern   = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
	uploadResourcePattern = regexp.MustCompile(`^/v1/uploads/(stg_[A-Za-z0-9_-]{16,96})$`)
	sha256Pattern         = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

type uploadTicket struct {
	claims   apiv1.DataPlaneTicketClaims
	uploadID string
}

type uploadTicketStore struct {
	mu         sync.Mutex
	instanceID string
	mode       string
	exposures  map[string]string
	service    *staging.Service
	items      map[string]uploadTicket
	now        func() time.Time
}

func newUploadTicketStore(configuration staging.DataPlaneConfiguration, service *staging.Service) *uploadTicketStore {
	return newModeUploadTicketStore(configuration, configuration.InstanceID, apiv1.ModeTicketRedirect, service)
}

func newModeUploadTicketStore(configuration staging.DataPlaneConfiguration, instanceID, mode string, service *staging.Service) *uploadTicketStore {
	exposures := make(map[string]string, len(configuration.Exposures))
	for _, binding := range configuration.Exposures {
		exposures[binding.TenantID] = binding.ExposureID
	}
	return &uploadTicketStore{instanceID: instanceID, mode: mode, exposures: exposures, service: service, items: map[string]uploadTicket{}, now: time.Now}
}

func (s *uploadTicketStore) install(ctx context.Context, call *contractv1.CallContext, raw []byte) error {
	if s == nil || s.service == nil {
		return errors.New("Content Upload 数据面未启用")
	}
	if call == nil || call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetCaller().GetId() != apiExposurePluginID || call.GetTenantId() == "" {
		return errors.New("Content Upload Ticket 只能由 API Exposure 安装")
	}
	var installation apiv1.DataPlaneTicketInstallation
	if err := decodeUploadJSON(raw, &installation); err != nil {
		return err
	}
	claims := installation.Claims
	match := uploadResourcePattern.FindStringSubmatch(claims.Resource)
	now := s.now().UTC()
	exposureID, bound := s.exposures[claims.TenantID]
	if !uploadTicketPattern.MatchString(installation.Ticket) || len(match) != 2 || !bound || claims.TenantID != call.GetTenantId() || claims.PrincipalID == "" || claims.Mode != s.mode || claims.DataPlaneExposureID != exposureID || claims.InstanceID != s.instanceID || claims.Method != http.MethodPut || !sha256Pattern.MatchString(claims.ContentSHA256) || !claims.ExpiresAt.After(now) || claims.ExpiresAt.Sub(now) > 35*time.Second {
		return errors.New("Content Upload Ticket 声明无效")
	}
	scope := staging.Scope{TenantID: claims.TenantID, ActorID: "user:" + claims.PrincipalID}
	status, err := s.service.UploadStatus(ctx, scope, match[1])
	if err != nil || (status.Upload.State != stagingv1.StatePending && status.Upload.State != stagingv1.StateUploading) || status.Upload.ExpectedDigest != claims.ContentSHA256 {
		return errors.New("Content Upload Ticket 未绑定可写 Upload Lease")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if len(s.items) >= 100_000 {
		return errors.New("Content Upload Ticket 容量达到上限")
	}
	if _, duplicate := s.items[installation.Ticket]; duplicate {
		return errors.New("Content Upload Ticket 重复")
	}
	s.items[installation.Ticket] = uploadTicket{claims: claims, uploadID: match[1]}
	return nil
}

func (s *uploadTicketStore) consume(request *http.Request) (uploadTicket, bool) {
	if s == nil || request == nil {
		return uploadTicket{}, false
	}
	values, exists := request.URL.Query()["vp_ticket"]
	if !exists || len(values) != 1 || !uploadTicketPattern.MatchString(values[0]) {
		return uploadTicket{}, false
	}
	query := request.URL.Query()
	query.Del("vp_ticket")
	if len(query) != 0 {
		return uploadTicket{}, false
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	ticket, ok := s.items[values[0]]
	if !ok || ticket.claims.Method != request.Method || ticket.claims.Resource != request.URL.EscapedPath() || !ticket.claims.ExpiresAt.After(now) {
		return uploadTicket{}, false
	}
	delete(s.items, values[0])
	request.URL.RawQuery = ""
	return ticket, true
}

func (s *uploadTicketStore) pruneLocked(now time.Time) {
	for token, ticket := range s.items {
		if !ticket.claims.ExpiresAt.After(now) {
			delete(s.items, token)
		}
	}
}

func decodeUploadJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("Content Upload 请求必须只有一个 JSON 文档")
	}
	return nil
}
