package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const apiExposureCapability = "platform.api-exposure"
const dataPlaneLeaseTTL = 240

type dataPlaneLeaseConfig struct {
	ExposureID  string `json:"exposureId"`
	InstanceID  string `json:"instanceId"`
	Endpoint    string `json:"endpoint"`
	TLSIdentity string `json:"tlsIdentity"`
}

type dataPlaneLeaseRegistrar struct {
	mu        sync.Mutex
	config    *dataPlaneLeaseConfig
	lease     apiv1.EndpointLease
	lastError string
}

func (r *dataPlaneLeaseRegistrar) ensure(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) {
	if r == nil || r.config == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.lease.LeaseID != "" && time.Until(r.lease.ExpiresAt) > 90*time.Second {
		return
	}
	if r.lease.LeaseID != "" {
		raw, _ := json.Marshal(apiv1.EndpointLeaseRenewal{LeaseID: r.lease.LeaseID, TTLSeconds: dataPlaneLeaseTTL})
		if lease, err := callEndpointLease(ctx, host, callCtx, "renewEndpointLease", raw); err == nil {
			r.lease, r.lastError = lease, ""
			return
		}
		r.lease = apiv1.EndpointLease{}
	}
	raw, _ := json.Marshal(apiv1.EndpointLeaseRegistration{
		DataPlaneExposureID: r.config.ExposureID, InstanceID: r.config.InstanceID, Endpoint: r.config.Endpoint, TLSIdentity: r.config.TLSIdentity,
		Modes: []string{apiv1.ModeTicketRedirect, apiv1.ModePrivateDirect}, TTLSeconds: dataPlaneLeaseTTL,
	})
	lease, err := callEndpointLease(ctx, host, callCtx, "registerEndpointLease", raw)
	if err != nil {
		r.lastError = err.Error()
		return
	}
	r.lease, r.lastError = lease, ""
}

func (r *dataPlaneLeaseRegistrar) status() map[string]any {
	if r == nil || r.config == nil {
		return map[string]any{"enabled": false}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return map[string]any{"enabled": true, "ready": r.lease.LeaseID != "" && r.lease.ExpiresAt.After(time.Now()), "leaseId": r.lease.LeaseID, "expiresAt": r.lease.ExpiresAt, "error": r.lastError}
}

func callEndpointLease(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, operation string, payload []byte) (apiv1.EndpointLease, error) {
	op := operation
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: apiExposureCapability, Operation: &op}, callCtx, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return apiv1.EndpointLease{}, errors.New("API Exposure Endpoint Lease 调用失败")
	}
	var lease apiv1.EndpointLease
	if json.Unmarshal(raw, &lease) != nil || apiv1.ValidateEndpointLease(lease, time.Now().UTC()) != nil {
		return apiv1.EndpointLease{}, errors.New("API Exposure 返回无效 Endpoint Lease")
	}
	return lease, nil
}

type dataPlaneTicketStore struct {
	mu         sync.Mutex
	instanceID string
	items      map[string]apiv1.DataPlaneTicketClaims
	now        func() time.Time
}

var dataPlaneTicketPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func newDataPlaneTicketStore(instanceID string) *dataPlaneTicketStore {
	return &dataPlaneTicketStore{instanceID: instanceID, items: map[string]apiv1.DataPlaneTicketClaims{}, now: time.Now}
}

func (s *dataPlaneTicketStore) install(callCtx *contractv1.CallContext, raw []byte) error {
	if callCtx.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.GetCaller().GetId() != "cn.vastplan.platform.integration.api-exposure" || callCtx.GetTenantId() == "" {
		return errors.New("Ticket 只能由 API Exposure 控制面安装")
	}
	var installation apiv1.DataPlaneTicketInstallation
	if err := decodeParams(raw, &installation); err != nil {
		return err
	}
	now := s.now().UTC()
	claims := installation.Claims
	if !dataPlaneTicketPattern.MatchString(installation.Ticket) || claims.TenantID != callCtx.GetTenantId() || claims.InstanceID != s.instanceID || claims.Method != http.MethodGet || claims.Resource == "" || !claims.ExpiresAt.After(now) || claims.ExpiresAt.Sub(now) > 35*time.Second {
		return errors.New("Data Plane Ticket 安装内容无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	if len(s.items) >= 100_000 {
		return errors.New("Data Plane Ticket 容量达到上限")
	}
	if _, duplicate := s.items[installation.Ticket]; duplicate {
		return errors.New("Data Plane Ticket 重复")
	}
	s.items[installation.Ticket] = claims
	return nil
}

func (s *dataPlaneTicketStore) consume(request *http.Request) bool {
	values, exists := request.URL.Query()["vp_ticket"]
	if !exists {
		return false
	}
	if len(values) != 1 || !dataPlaneTicketPattern.MatchString(values[0]) {
		return false
	}
	query := request.URL.Query()
	query.Del("vp_ticket")
	resource := request.URL.EscapedPath()
	if encoded := query.Encode(); encoded != "" {
		resource += "?" + encoded
	}
	now := s.now().UTC()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(now)
	claims, ok := s.items[values[0]]
	if !ok || claims.Method != request.Method || claims.Resource != resource || !claims.ExpiresAt.After(now) {
		return false
	}
	delete(s.items, values[0])
	request.URL.RawQuery = query.Encode()
	return true
}

func (s *dataPlaneTicketStore) pruneLocked(now time.Time) {
	for token, claims := range s.items {
		if !claims.ExpiresAt.After(now) {
			delete(s.items, token)
		}
	}
}

func dataPlaneTicketMiddleware(next http.Handler, tickets *dataPlaneTicketStore, readToken string) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, present := request.URL.Query()["vp_ticket"]; present {
			if tickets == nil || !tickets.consume(request) {
				http.Error(response, "unauthorized", http.StatusUnauthorized)
				return
			}
			request.Header.Set("Authorization", "Bearer "+readToken)
			response.Header().Set("Referrer-Policy", "no-referrer")
			response.Header().Set("Cache-Control", "private, no-store")
		}
		next.ServeHTTP(response, request)
	})
}

func validateDataPlaneLeaseConfig(config *dataPlaneLeaseConfig) error {
	if config == nil {
		return nil
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.Host == "" || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return errors.New("apiExposure.endpoint 必须是无凭据和 query 的 HTTPS URL")
	}
	if config.ExposureID == "" || config.InstanceID == "" || config.TLSIdentity == "" {
		return fmt.Errorf("apiExposure 配置不完整")
	}
	return nil
}
