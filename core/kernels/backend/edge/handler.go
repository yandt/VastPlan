// Package edge provides the stable browser boundary. It authenticates before any
// portal-control call and never accepts Principal or tenant fields from request JSON.
package edge

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const csrfCookieName = "vastplan_csrf"

type IdentityProvider interface {
	Authenticate(*http.Request) (portalapi.Principal, error)
}

type Handler struct {
	identity    IdentityProvider
	service     portalapi.Service
	interaction InteractionService
	catalog     *TrustedCatalog
	assets      http.Handler
}

func New(identity IdentityProvider, service portalapi.Service) *Handler {
	return NewWithInteraction(identity, service, nil)
}

func NewWithInteraction(identity IdentityProvider, service portalapi.Service, interaction InteractionService) *Handler {
	return NewWithRuntime(identity, service, interaction, nil)
}

// NewWithRuntime enables the governed browser bootstrap and module endpoints.
// Existing control-plane-only embeddings may keep catalog nil; production
// Portal Edge always injects the trusted catalog.
func NewWithRuntime(identity IdentityProvider, service portalapi.Service, interaction InteractionService, catalog *TrustedCatalog) *Handler {
	return NewPortal(identity, service, interaction, catalog, nil)
}

// NewPortal assembles both the authenticated BFF and the deployable shell.
// API paths never fall back to static content; client-side routes do.
func NewPortal(identity IdentityProvider, service portalapi.Service, interaction InteractionService, catalog *TrustedCatalog, assets http.Handler) *Handler {
	if identity == nil || service == nil {
		panic("Edge BFF 必须注入身份提供方和 Portal 服务")
	}
	return &Handler{identity: identity, service: service, interaction: interaction, catalog: catalog, assets: assets}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Path == "/v1/csrf" {
		h.csrf(w, r)
		return
	}
	runtimePath := r.URL.Path == "/v1/portal-runtime" || strings.HasPrefix(r.URL.Path, "/v1/portal-modules/")
	portalPath := strings.HasPrefix(r.URL.Path, "/v1/portal-drafts")
	interactionPath := strings.HasPrefix(r.URL.Path, "/v1/interactions")
	if !runtimePath && !portalPath && !interactionPath {
		if h.assets != nil && !strings.HasPrefix(r.URL.Path, "/v1/") && r.URL.Path != "/v1" {
			h.assets.ServeHTTP(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}
	p, err := h.identity.Authenticate(r)
	if err != nil || p.ID == "" || p.TenantID == "" {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && !validCSRF(r) {
		writeError(w, http.StatusForbidden, "csrf_rejected")
		return
	}
	if runtimePath {
		h.runtimeRoute(w, r, p)
		return
	}
	if portalPath {
		h.route(w, r, p)
		return
	}
	h.interactionRoute(w, r, p)
}

func (h *Handler) runtimeRoute(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if h.catalog == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w)
		return
	}
	revisions, err := h.service.List(r.Context(), p)
	if err != nil {
		respond(w, nil, err)
		return
	}
	if r.URL.Path == "/v1/portal-runtime" {
		revision, ok := selectActivePortal(revisions, p.TenantID, r.URL.Query().Get("path"), requestHost(r))
		if !ok {
			writeError(w, http.StatusNotFound, "portal_not_found")
			return
		}
		runtime, err := h.catalog.ResolveRuntime(r.Context(), p.TenantID, revision.Spec)
		if err != nil {
			writeError(w, http.StatusConflict, "portal_runtime_rejected")
			return
		}
		writeJSON(w, http.StatusOK, runtime)
		return
	}
	revisionID, pluginID, ok := parseModulePath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	revision, ok := activeRevision(revisions, p.TenantID, revisionID)
	if !ok {
		writeError(w, http.StatusNotFound, "portal_revision_not_found")
		return
	}
	asset, err := h.catalog.ReadFrontendModule(r.Context(), p.TenantID, revision.Spec, pluginID)
	if err != nil {
		writeError(w, http.StatusNotFound, "portal_module_not_found")
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("ETag", `"sha256-`+asset.Descriptor.SHA256+`"`)
	w.Header().Set("X-VastPlan-Module-SHA256", asset.Descriptor.SHA256)
	w.Header().Set("X-VastPlan-Package-SHA256", asset.Descriptor.PackageSHA256)
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(asset.Content)
	}
}

func selectActivePortal(revisions []portalapi.Revision, tenantID, requestedPath, host string) (portalapi.Revision, bool) {
	if requestedPath == "" {
		requestedPath = "/"
	}
	if !strings.HasPrefix(requestedPath, "/") {
		return portalapi.Revision{}, false
	}
	var selected portalapi.Revision
	for _, revision := range revisions {
		if !isActiveRevision(revision, tenantID) || !routeMatches(revision.Spec.Route, requestedPath) || !domainMatches(revision.Spec.Domains, host) {
			continue
		}
		if selected.ID == 0 || len(revision.Spec.Route) > len(selected.Spec.Route) {
			selected = revision
		}
	}
	return selected, selected.ID != 0
}

func activeRevision(revisions []portalapi.Revision, tenantID string, id uint64) (portalapi.Revision, bool) {
	for _, revision := range revisions {
		if revision.ID == id && isActiveRevision(revision, tenantID) {
			return revision, true
		}
	}
	return portalapi.Revision{}, false
}

func isActiveRevision(revision portalapi.Revision, tenantID string) bool {
	return revision.ID != 0 && revision.TenantID == tenantID && revision.Active && revision.Status == portalapi.StatusPublished && revision.Spec.Revision == revision.ID
}

func routeMatches(root, requested string) bool {
	if root == "/" {
		return true
	}
	root = strings.TrimSuffix(root, "/")
	return requested == root || strings.HasPrefix(requested, root+"/")
}

func domainMatches(domains []string, host string) bool {
	if len(domains) == 0 {
		return true
	}
	for _, domain := range domains {
		if strings.EqualFold(domain, host) {
			return true
		}
	}
	return false
}

func requestHost(r *http.Request) string {
	value := r.Host
	if parsed, err := url.Parse("//" + value); err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return value
}

func parseModulePath(value string) (uint64, string, bool) {
	parts := strings.Split(strings.TrimPrefix(value, "/v1/portal-modules/"), "/")
	if len(parts) != 2 || parts[0] == "" || !strings.HasSuffix(parts[1], ".js") {
		return 0, "", false
	}
	var revision uint64
	if _, err := fmtSscan(parts[0], &revision); err != nil || revision == 0 {
		return 0, "", false
	}
	pluginID := strings.TrimSuffix(parts[1], ".js")
	if pluginID == "" || strings.ContainsAny(pluginID, "/\\") {
		return 0, "", false
	}
	return revision, pluginID, true
}

func (h *Handler) interactionRoute(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if h.interaction == nil {
		http.NotFound(w, r)
		return
	}
	if r.URL.Path == "/v1/interactions" {
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.interaction.List(r.Context(), p)
		respondInteraction(w, value, err)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/interactions"), "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] != "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[1]
	if len(parts) == 2 {
		if r.Method == http.MethodGet {
			value, err := h.interaction.Get(r.Context(), p, id)
			respondInteraction(w, value, err)
			return
		}
		methodNotAllowed(w)
		return
	}
	switch parts[2] {
	case "present":
		if r.Method == http.MethodPost {
			value, err := h.interaction.Present(r.Context(), p, id)
			respondInteraction(w, value, err)
			return
		}
	case "respond":
		if r.Method == http.MethodPost {
			var response uiv1.InteractionResponse
			if !decode(w, r, &response) {
				return
			}
			value, err := h.interaction.Respond(r.Context(), p, id, response)
			respondInteraction(w, value, err)
			return
		}
	}
	methodNotAllowed(w)
}

func (h *Handler) csrf(w http.ResponseWriter, r *http.Request) {
	p, err := h.identity.Authenticate(r)
	if err != nil || p.ID == "" || p.TenantID == "" {
		writeError(w, http.StatusUnauthorized, "session_required")
		return
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		writeError(w, http.StatusInternalServerError, "csrf_unavailable")
		return
	}
	token := hex.EncodeToString(b)
	http.SetCookie(w, &http.Cookie{Name: csrfCookieName, Value: token, Path: "/", Secure: true, SameSite: http.SameSiteStrictMode, HttpOnly: false, MaxAge: 900})
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (h *Handler) route(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/portal-drafts"), "/")
	if r.URL.Path == "/v1/portal-drafts" {
		switch r.Method {
		case http.MethodGet:
			h.list(w, r, p)
		case http.MethodPost:
			h.create(w, r, p)
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(parts) != 3 || parts[0] != "" || parts[1] == "" {
		http.NotFound(w, r)
		return
	}
	var id uint64
	if _, err := fmtSscan(parts[1], &id); err != nil || id == 0 {
		writeError(w, http.StatusBadRequest, "invalid_revision")
		return
	}
	switch parts[2] {
	case "submit":
		if r.Method == http.MethodPost {
			h.submit(w, r, p, id)
			return
		}
	case "approve":
		if r.Method == http.MethodPost {
			h.approve(w, r, p, id)
			return
		}
	case "publish":
		if r.Method == http.MethodPost {
			h.publish(w, r, p, id)
			return
		}
	case "rollback":
		if r.Method == http.MethodPost {
			h.rollback(w, r, p, id)
			return
		}
	case "audit":
		if r.Method == http.MethodGet {
			h.audit(w, r, p, id)
			return
		}
	}
	methodNotAllowed(w)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	var composition frontendcompositionv1.ApplicationComposition
	if !decode(w, r, &composition) {
		return
	}
	v, err := h.service.CreateDraft(r.Context(), p, composition)
	respond(w, v, err)
}
func (h *Handler) list(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	v, err := h.service.List(r.Context(), p)
	respond(w, v, err)
}
func (h *Handler) submit(w http.ResponseWriter, r *http.Request, p portalapi.Principal, id uint64) {
	v, err := h.service.Submit(r.Context(), p, id)
	respond(w, v, err)
}
func (h *Handler) approve(w http.ResponseWriter, r *http.Request, p portalapi.Principal, id uint64) {
	v, err := h.service.Approve(r.Context(), p, id)
	respond(w, v, err)
}
func (h *Handler) publish(w http.ResponseWriter, r *http.Request, p portalapi.Principal, id uint64) {
	var request portalapi.PublishRequest
	if !decode(w, r, &request) {
		return
	}
	v, err := h.service.Publish(r.Context(), p, id, request)
	respond(w, v, err)
}
func (h *Handler) rollback(w http.ResponseWriter, r *http.Request, p portalapi.Principal, id uint64) {
	var request portalapi.PublishRequest
	if !decode(w, r, &request) {
		return
	}
	v, err := h.service.Rollback(r.Context(), p, id, request)
	respond(w, v, err)
}
func (h *Handler) audit(w http.ResponseWriter, r *http.Request, p portalapi.Principal, id uint64) {
	v, err := h.service.Audit(r.Context(), p, id)
	respond(w, v, err)
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	de := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	de.DisallowUnknownFields()
	if err := de.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json")
		return false
	}
	return true
}
func respond(w http.ResponseWriter, v any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, v)
		return
	}
	var capabilityErr *CapabilityError
	if errors.As(err, &capabilityErr) && capabilityErr.Code == errorcode.PermissionDenied {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	switch {
	case errors.Is(err, portalapi.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, portalapi.ErrForbidden), errors.Is(err, portalapi.ErrSelfApproval):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, portalapi.ErrInvalidState), errors.Is(err, portalapi.ErrRouteConflict), errors.Is(err, portalapi.ErrCatalogRejected):
		writeError(w, http.StatusConflict, "transition_rejected")
	default:
		writeError(w, http.StatusBadRequest, "request_rejected")
	}
}

func respondInteraction(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	var capabilityErr *CapabilityError
	if errors.As(err, &capabilityErr) && capabilityErr.Code == errorcode.PermissionDenied {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	switch {
	case errors.Is(err, interactionapi.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, interactionapi.ErrForbidden):
		writeError(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, interactionapi.ErrConflict), errors.Is(err, interactionapi.ErrInvalidState), errors.Is(err, interactionapi.ErrExpired):
		writeError(w, http.StatusConflict, "transition_rejected")
	default:
		writeError(w, http.StatusBadRequest, "request_rejected")
	}
}
func validCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookieName)
	h := r.Header.Get("X-VastPlan-CSRF")
	return err == nil && h != "" && len(c.Value) == len(h) && subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) == 1
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
func methodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, "method_not_allowed")
}

// isolated for tests; avoids accepting float or signed IDs.
func fmtSscan(s string, out *uint64) (int, error) {
	var n uint64
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return 0, errors.New("not decimal")
		}
		n = n*10 + uint64(ch-'0')
	}
	*out = n
	return 1, nil
}
