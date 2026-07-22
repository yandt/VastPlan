package apiexposure

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
)

const maxLeaseTTL = 300
const maxEndpointLeases = 100_000
const maxOutstandingTickets = 100_000

var sha256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type ticketRecord struct {
	Claims   TicketClaims
	LeaseID  string
	PluginID string
}

func (s *Service) RegisterEndpointLease(_ context.Context, caller RuntimeCaller, request EndpointLeaseRequest) (apiv1.EndpointLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.pruneEphemeralLocked(now)
	exposure, service, err := s.authorizeLeaseLocked(caller, request.DataPlaneExposureID, request.Modes)
	if err != nil {
		return apiv1.EndpointLease{}, err
	}
	if request.TTLSeconds == 0 || request.TTLSeconds > maxLeaseTTL || !subset(request.Modes, exposure.AllowedModes) || !subset(request.Modes, service.SupportedModes) {
		return apiv1.EndpointLease{}, errors.New("Endpoint Lease 模式或 TTL 超出已发布边界")
	}
	endpoint, err := url.Parse(request.Endpoint)
	if err != nil || !slices.Contains(exposure.AllowedEndpointOrigins, "https://"+strings.ToLower(endpoint.Host)) || !strings.HasPrefix(request.TLSIdentity, exposure.TLSIdentityPrefix) || request.TLSIdentity == exposure.TLSIdentityPrefix {
		return apiv1.EndpointLease{}, errors.New("Endpoint Lease 超出已审批的 endpoint 或 TLS identity 边界")
	}
	id, err := randomToken(24)
	if err != nil {
		return apiv1.EndpointLease{}, err
	}
	lease := apiv1.EndpointLease{
		SchemaVersion: apiv1.SchemaVersion, LeaseID: "lease_" + id, DataPlaneExposureID: exposure.ID,
		InstanceID: request.InstanceID, Endpoint: request.Endpoint, TLSIdentity: request.TLSIdentity,
		Modes: append([]string(nil), request.Modes...), IssuedAt: now, ExpiresAt: now.Add(time.Duration(request.TTLSeconds) * time.Second),
	}
	if err := apiv1.ValidateEndpointLease(lease, now); err != nil {
		return apiv1.EndpointLease{}, err
	}
	for leaseID, existing := range s.leases {
		if s.leaseOwners[leaseID] == caller && existing.DataPlaneExposureID == exposure.ID && existing.InstanceID == request.InstanceID {
			delete(s.leases, leaseID)
			delete(s.leaseOwners, leaseID)
		}
	}
	if len(s.leases) >= maxEndpointLeases {
		return apiv1.EndpointLease{}, errors.New("Endpoint Lease 容量达到上限")
	}
	s.leases[lease.LeaseID], s.leaseOwners[lease.LeaseID] = lease, caller
	return lease, nil
}

func (s *Service) RenewEndpointLease(_ context.Context, caller RuntimeCaller, request EndpointLeaseRenewal) (apiv1.EndpointLease, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	s.pruneEphemeralLocked(now)
	lease, ok := s.leases[request.LeaseID]
	owner := s.leaseOwners[request.LeaseID]
	if !ok || owner != caller || request.TTLSeconds == 0 || request.TTLSeconds > maxLeaseTTL {
		return apiv1.EndpointLease{}, ErrForbidden
	}
	lease.IssuedAt, lease.ExpiresAt = now, now.Add(time.Duration(request.TTLSeconds)*time.Second)
	if err := apiv1.ValidateEndpointLease(lease, now); err != nil {
		return apiv1.EndpointLease{}, err
	}
	s.leases[lease.LeaseID] = lease
	return lease, nil
}

func (s *Service) RevokeEndpointLease(_ context.Context, caller RuntimeCaller, request EndpointLeaseRevocation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.leaseOwners[request.LeaseID] != caller {
		return ErrForbidden
	}
	delete(s.leases, request.LeaseID)
	delete(s.leaseOwners, request.LeaseID)
	return nil
}

func (s *Service) IssueTicket(_ context.Context, principal Principal, request TicketRequest) (TicketGrant, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	grant, _, err := s.issueTicketLocked(principal, request)
	return grant, err
}

func (s *Service) IssueTicketInstallation(_ context.Context, principal Principal, request TicketRequest) (TicketGrant, TicketInstallation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.issueTicketLocked(principal, request)
}

func (s *Service) issueTicketLocked(principal Principal, request TicketRequest) (TicketGrant, TicketInstallation, error) {
	now := s.now().UTC()
	s.pruneEphemeralLocked(now)
	exposure, ok := s.activeDataPlaneLocked(principal.TenantID, request.DataPlaneExposureID)
	if !ok || !hasAll(principal.Roles, exposure.RequiredPermissions) || !validTicketResource(request) {
		return TicketGrant{}, TicketInstallation{}, ErrForbidden
	}
	service, err := s.resolveDataPlaneService(exposure.Service)
	if err != nil || service.Service.TicketTarget == nil {
		return TicketGrant{}, TicketInstallation{}, ErrForbidden
	}
	candidates := make([]apiv1.EndpointLease, 0)
	for id, lease := range s.leases {
		owner := s.leaseOwners[id]
		if lease.DataPlaneExposureID == exposure.ID && owner.TenantID == principal.TenantID && slices.Contains(lease.Modes, apiv1.ModeTicketRedirect) {
			candidates = append(candidates, lease)
		}
	}
	if len(candidates) == 0 {
		return TicketGrant{}, TicketInstallation{}, errors.New("Data Plane 暂无可用 Endpoint Lease")
	}
	if len(s.tickets) >= maxOutstandingTickets {
		return TicketGrant{}, TicketInstallation{}, errors.New("Data Plane Ticket 容量达到上限")
	}
	s.leaseCursor++
	lease := candidates[int(s.leaseCursor%uint64(len(candidates)))]
	token, err := randomToken(32)
	if err != nil {
		return TicketGrant{}, TicketInstallation{}, err
	}
	expires := now.Add(30 * time.Second)
	claims := TicketClaims{TenantID: principal.TenantID, PrincipalID: principal.ID, DataPlaneExposureID: exposure.ID, InstanceID: lease.InstanceID, Method: request.Method, Resource: request.Resource, ContentSHA256: request.ContentSHA256, ExpiresAt: expires}
	s.tickets[token] = ticketRecord{Claims: claims, LeaseID: lease.LeaseID, PluginID: s.leaseOwners[lease.LeaseID].PluginID}
	grant := TicketGrant{Endpoint: lease.Endpoint, LeaseID: lease.LeaseID, Ticket: token, ExpiresAt: expires}
	installation := TicketInstallation{Target: *service.Service.TicketTarget, Ticket: token, Claims: claims}
	return grant, installation, nil
}

func (s *Service) CancelTicket(ticket string) {
	s.mu.Lock()
	delete(s.tickets, ticket)
	s.mu.Unlock()
}

func (s *Service) ConsumeTicket(_ context.Context, caller RuntimeCaller, request TicketConsumption) (TicketClaims, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now().UTC()
	record, ok := s.tickets[request.Ticket]
	if !ok || record.PluginID != caller.PluginID || record.Claims.TenantID != caller.TenantID || record.Claims.InstanceID != request.InstanceID || !record.Claims.ExpiresAt.After(now) {
		return TicketClaims{}, ErrForbidden
	}
	lease, ok := s.leases[record.LeaseID]
	if !ok || lease.InstanceID != request.InstanceID || !lease.ExpiresAt.After(now) {
		return TicketClaims{}, ErrForbidden
	}
	delete(s.tickets, request.Ticket)
	return record.Claims, nil
}

func (s *Service) authorizeLeaseLocked(caller RuntimeCaller, exposureID string, modes []string) (apiv1.DataPlaneExposure, apiv1.DataPlaneServiceContribution, error) {
	exposure, ok := s.activeDataPlaneLocked(caller.TenantID, exposureID)
	if !ok || exposure.Service.PluginID != caller.PluginID {
		return apiv1.DataPlaneExposure{}, apiv1.DataPlaneServiceContribution{}, ErrForbidden
	}
	resolved, err := s.resolveDataPlaneService(exposure.Service)
	if err != nil || !subset(modes, exposure.AllowedModes) {
		return apiv1.DataPlaneExposure{}, apiv1.DataPlaneServiceContribution{}, ErrForbidden
	}
	return exposure, resolved.Service, nil
}

func (s *Service) activeDataPlaneLocked(tenantID, exposureID string) (apiv1.DataPlaneExposure, bool) {
	for _, revision := range s.state.DataPlaneRevisions {
		if revision.Status == StatusPublished && revision.Exposure.TenantID == tenantID && revision.Exposure.ID == exposureID {
			return revision.Exposure, true
		}
	}
	return apiv1.DataPlaneExposure{}, false
}

func (s *Service) pruneEphemeralLocked(now time.Time) {
	for id, lease := range s.leases {
		if !lease.ExpiresAt.After(now) {
			delete(s.leases, id)
			delete(s.leaseOwners, id)
		}
	}
	for token, ticket := range s.tickets {
		if !ticket.Claims.ExpiresAt.After(now) {
			delete(s.tickets, token)
		}
	}
}

func validTicketResource(request TicketRequest) bool {
	if request.Method != "GET" && request.Method != "PUT" {
		return false
	}
	if !strings.HasPrefix(request.Resource, "/") || strings.HasPrefix(request.Resource, "//") || len(request.Resource) > 2048 || strings.Contains(request.Resource, "\\") || strings.ContainsAny(request.Resource, "\r\n\x00") {
		return false
	}
	return request.ContentSHA256 == "" || sha256Pattern.MatchString(request.ContentSHA256)
}

func randomToken(bytes int) (string, error) {
	raw := make([]byte, bytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
func hasAll(actual, required []string) bool {
	return !slices.ContainsFunc(required, func(value string) bool { return !slices.Contains(actual, value) })
}
