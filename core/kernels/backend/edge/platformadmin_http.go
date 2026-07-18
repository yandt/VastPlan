package edge

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func (h *Handler) resolvePlatformRequest(w http.ResponseWriter, r *http.Request, p portalapi.Principal) (portalapi.ManagementTarget, []string, bool) {
	if h.platform == nil {
		http.NotFound(w, r)
		return portalapi.ManagementTarget{}, nil, false
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/portals/"), "/")
	if len(parts) < 5 || parts[1] != "platform" || parts[2] != "services" {
		http.NotFound(w, r)
		return portalapi.ManagementTarget{}, nil, false
	}
	portalID, portalErr := url.PathUnescape(parts[0])
	serviceID, serviceErr := url.PathUnescape(parts[3])
	if portalErr != nil || serviceErr != nil || validResourceName(portalID, 128) != nil || validResourceName(serviceID, 160) != nil {
		writeError(w, http.StatusBadRequest, "invalid_management_target")
		return portalapi.ManagementTarget{}, nil, false
	}
	revisions, err := h.service.List(r.Context(), p)
	if err != nil {
		respond(w, nil, err)
		return portalapi.ManagementTarget{}, nil, false
	}
	revision, ok := activePortalByID(revisions, p.TenantID, portalID, requestHost(r))
	if !ok {
		writeError(w, http.StatusNotFound, "portal_not_found")
		return portalapi.ManagementTarget{}, nil, false
	}
	if !audienceAllows(revision.Spec.Audience, p) {
		writeError(w, http.StatusForbidden, "portal_audience_forbidden")
		return portalapi.ManagementTarget{}, nil, false
	}
	binding := revision.Spec.Management
	if frontendcompositionv1.ValidatePortalBinding(binding) != nil || binding.PlatformProfile != revision.Spec.Resolution.PlatformProfile || compositioncommonv1.Digest(binding) != revision.Spec.Resolution.ManagementBindingDigest {
		writeError(w, http.StatusConflict, "portal_management_binding_rejected")
		return portalapi.ManagementTarget{}, nil, false
	}
	target, ok := revision.Spec.ManagementTarget(serviceID)
	if !ok {
		writeError(w, http.StatusNotFound, "managed_service_not_found")
		return portalapi.ManagementTarget{}, nil, false
	}
	return target, parts[4:], true
}

func (h *Handler) platformRoute(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget, parts []string) {
	if len(parts) >= 2 && parts[0] == "deployment" {
		h.deploymentRoute(w, r, p, target, parts[1:])
		return
	}
	if len(parts) == 1 {
		switch parts[0] {
		case "settings":
			h.listSettings(w, r, p, target)
			return
		case "credentials":
			h.listCredentials(w, r, p, target)
			return
		case "database-connections":
			h.listDatabaseConnections(w, r, p, target)
			return
		}
	}
	if len(parts) == 2 && parts[0] == "artifacts" && parts[1] == "status" {
		if !requireManagementOperation(w, target, platformadminapi.ArtifactsCapability, "status", false) {
			return
		}
		if !requirePlatformRole(w, p, "platform.artifacts.read") {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.platform.ArtifactRepositoryStatus(r.Context(), p, target)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) < 2 || len(parts) > 3 {
		http.NotFound(w, r)
		return
	}
	name, err := url.PathUnescape(parts[1])
	if err != nil || validResourceName(name, 320) != nil {
		writeError(w, http.StatusBadRequest, "invalid_resource_name")
		return
	}
	switch parts[0] {
	case "settings":
		h.settingItem(w, r, p, target, name, parts)
	case "credentials":
		h.credentialItem(w, r, p, target, name, parts)
	case "database-connections":
		h.databaseConnectionItem(w, r, p, target, name, parts)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) deploymentRoute(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget, parts []string) {
	if len(parts) == 1 && parts[0] == "targets" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "listDeploymentTargets", false) || !requirePlatformRole(w, p, "platform.deployment.read") {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.platform.ListDeploymentTargets(r.Context(), p, target)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 1 && parts[0] == "service-revisions" {
		switch r.Method {
		case http.MethodGet:
			if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "listServiceRevisions", false) || !requirePlatformRole(w, p, "platform.deployment.read") {
				return
			}
			value, err := h.platform.ListServiceRevisions(r.Context(), p, target)
			respondPlatform(w, value, err)
		case http.MethodPost:
			if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "createServiceDraft", true) || !requirePlatformRole(w, p, "platform.deployment.compose") {
				return
			}
			var request platformadminapi.ServiceCompositionRequest
			if !decode(w, r, &request) {
				return
			}
			value, err := h.platform.CreateServiceDraft(r.Context(), p, target, request)
			respondPlatform(w, value, err)
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(parts) >= 2 && parts[0] == "service-revisions" {
		id, ok := deploymentRevisionID(w, parts[1])
		if !ok {
			return
		}
		if len(parts) == 2 {
			if r.Method != http.MethodPut {
				methodNotAllowed(w)
				return
			}
			if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "updateServiceDraft", true) || !requirePlatformRole(w, p, "platform.deployment.compose") {
				return
			}
			var request platformadminapi.ServiceCompositionRequest
			if !decode(w, r, &request) {
				return
			}
			value, err := h.platform.UpdateServiceDraft(r.Context(), p, target, id, request)
			respondPlatform(w, value, err)
			return
		}
		if len(parts) != 3 {
			http.NotFound(w, r)
			return
		}
		if parts[2] == "audit" {
			if r.Method != http.MethodGet {
				methodNotAllowed(w)
				return
			}
			if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "listServiceRevisionAudit", false) || !requirePlatformRole(w, p, "platform.deployment.read") {
				return
			}
			value, err := h.platform.ListServiceRevisionAudit(r.Context(), p, target, id)
			respondPlatform(w, value, err)
			return
		}
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		var operation, role string
		var action func() (platformadminapi.ServiceRevision, error)
		switch parts[2] {
		case "submit":
			operation, role = "submitServiceDraft", "platform.deployment.compose"
			action = func() (platformadminapi.ServiceRevision, error) {
				return h.platform.SubmitServiceDraft(r.Context(), p, target, id)
			}
		case "approve":
			operation, role = "approveServiceRevision", "platform.deployment.approve"
			action = func() (platformadminapi.ServiceRevision, error) {
				return h.platform.ApproveServiceRevision(r.Context(), p, target, id)
			}
		case "publish":
			operation, role = "publishServiceRevision", "platform.deployment.publish"
			action = func() (platformadminapi.ServiceRevision, error) {
				return h.platform.PublishServiceRevision(r.Context(), p, target, id)
			}
		case "rollback":
			operation, role = "rollbackServiceRevision", "platform.deployment.publish"
			action = func() (platformadminapi.ServiceRevision, error) {
				return h.platform.RollbackServiceRevision(r.Context(), p, target, id)
			}
		default:
			http.NotFound(w, r)
			return
		}
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, operation, true) || !requirePlatformRole(w, p, role) {
			return
		}
		value, err := action()
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 1 && parts[0] == "nodes" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "listNodes", false) {
			return
		}
		if !requirePlatformRole(w, p, "platform.deployment.read") {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.platform.ListManagedNodes(r.Context(), p, target)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 1 && parts[0] == "bootstrap-jobs" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "listBootstrapJobs", false) {
			return
		}
		if !requirePlatformRole(w, p, "platform.deployment.read") {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.platform.ListBootstrapJobs(r.Context(), p, target)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 2 && parts[0] == "nodes" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "putNode", true) {
			return
		}
		if !requirePlatformRole(w, p, "platform.deployment.write") {
			return
		}
		if r.Method != http.MethodPut {
			methodNotAllowed(w)
			return
		}
		id, ok := deploymentResourceName(w, parts[1])
		if !ok {
			return
		}
		var request platformadminapi.PutManagedNodeRequest
		if !decode(w, r, &request) {
			return
		}
		value, err := h.platform.PutManagedNode(r.Context(), p, target, id, request)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 3 && parts[0] == "nodes" && parts[2] == "bootstrap" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "createBootstrap", true) {
			return
		}
		if !requirePlatformRole(w, p, "platform.deployment.bootstrap") {
			return
		}
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		id, ok := deploymentResourceName(w, parts[1])
		if !ok {
			return
		}
		value, err := h.platform.CreateBootstrapJob(r.Context(), p, target, id)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 3 && parts[0] == "bootstrap-jobs" && parts[2] == "approve" {
		if !requireManagementOperation(w, target, platformadminapi.DeploymentCapability, "approveBootstrap", true) {
			return
		}
		if !requirePlatformRole(w, p, "platform.deployment.approve") {
			return
		}
		if r.Method != http.MethodPost {
			methodNotAllowed(w)
			return
		}
		id, ok := deploymentResourceName(w, parts[1])
		if !ok {
			return
		}
		value, err := h.platform.ApproveBootstrapJob(r.Context(), p, target, id)
		respondPlatform(w, value, err)
		return
	}
	http.NotFound(w, r)
}

func deploymentRevisionID(w http.ResponseWriter, raw string) (uint64, bool) {
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil || value == 0 {
		writeError(w, http.StatusBadRequest, "invalid_revision_id")
		return 0, false
	}
	return value, true
}

func deploymentResourceName(w http.ResponseWriter, raw string) (string, bool) {
	value, err := url.PathUnescape(raw)
	if err != nil || validResourceName(value, 128) != nil {
		writeError(w, http.StatusBadRequest, "invalid_resource_name")
		return "", false
	}
	return value, true
}

func (h *Handler) listSettings(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget) {
	if !requireManagementOperation(w, target, platformadminapi.SettingsCapability, "list", false) {
		return
	}
	if !requirePlatformRole(w, p, "platform.settings.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListSettings(r.Context(), p, target, r.URL.Query().Get("prefix"))
	respondPlatform(w, value, err)
}

func (h *Handler) settingItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget, key string, parts []string) {
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	if !requirePlatformRole(w, p, "platform.admin") {
		return
	}
	switch r.Method {
	case http.MethodPut:
		if !requireManagementOperation(w, target, platformadminapi.SettingsCapability, "put", true) {
			return
		}
		var request platformadminapi.PutSettingRequest
		if !decode(w, r, &request) {
			return
		}
		value, err := h.platform.PutSetting(r.Context(), p, target, key, request)
		respondPlatform(w, value, err)
	case http.MethodDelete:
		if !requireManagementOperation(w, target, platformadminapi.SettingsCapability, "delete", true) {
			return
		}
		version, ok := optionalVersion(w, r)
		if !ok {
			return
		}
		respondPlatform(w, map[string]bool{"deleted": true}, h.platform.DeleteSetting(r.Context(), p, target, key, version))
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) listCredentials(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget) {
	if !requireManagementOperation(w, target, platformadminapi.CredentialsCapability, "list", false) {
		return
	}
	if !requirePlatformRole(w, p, "platform.credentials.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListCredentials(r.Context(), p, target, r.URL.Query().Get("prefix"))
	respondPlatform(w, value, err)
}

func (h *Handler) credentialItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget, name string, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodPut {
		if !requireManagementOperation(w, target, platformadminapi.CredentialsCapability, "put", true) {
			return
		}
		if !requirePlatformRole(w, p, "platform.credentials.write") {
			return
		}
		var request platformadminapi.PutCredentialRequest
		if !decode(w, r, &request) {
			return
		}
		value, err := h.platform.PutCredential(r.Context(), p, target, name, request)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPost {
		var value platformadminapi.CredentialMetadata
		var err error
		switch parts[2] {
		case "rotate":
			if !requireManagementOperation(w, target, platformadminapi.CredentialsCapability, "rotate", true) {
				return
			}
			if !requirePlatformRole(w, p, "platform.credentials.rotate") {
				return
			}
			value, err = h.platform.RotateCredential(r.Context(), p, target, name)
		case "revoke":
			if !requireManagementOperation(w, target, platformadminapi.CredentialsCapability, "revoke", true) {
				return
			}
			if !requirePlatformRole(w, p, "platform.credentials.revoke") {
				return
			}
			value, err = h.platform.RevokeCredential(r.Context(), p, target, name)
		default:
			http.NotFound(w, r)
			return
		}
		respondPlatform(w, value, err)
		return
	}
	methodNotAllowed(w)
}

func (h *Handler) listDatabaseConnections(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget) {
	if !requireManagementOperation(w, target, platformadminapi.DatabaseCapability, "list", false) {
		return
	}
	if !requirePlatformRole(w, p, "platform.database.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListDatabaseConnections(r.Context(), p, target)
	respondPlatform(w, value, err)
}

func (h *Handler) databaseConnectionItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, target portalapi.ManagementTarget, name string, parts []string) {
	if len(parts) == 2 {
		if !requirePlatformRole(w, p, "platform.database.write") {
			return
		}
		switch r.Method {
		case http.MethodPut:
			if !requireManagementOperation(w, target, platformadminapi.DatabaseCapability, "define", true) {
				return
			}
			var request platformadminapi.DatabaseConnection
			if !decode(w, r, &request) {
				return
			}
			value, err := h.platform.PutDatabaseConnection(r.Context(), p, target, name, request)
			respondPlatform(w, value, err)
		case http.MethodDelete:
			if !requireManagementOperation(w, target, platformadminapi.DatabaseCapability, "remove", true) {
				return
			}
			respondPlatform(w, map[string]bool{"deleted": true}, h.platform.DeleteDatabaseConnection(r.Context(), p, target, name))
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(parts) == 3 && parts[2] == "probe" && r.Method == http.MethodPost {
		if !requireManagementOperation(w, target, platformadminapi.DatabaseCapability, "probe", true) {
			return
		}
		if !requirePlatformRole(w, p, "platform.database.probe") {
			return
		}
		value, err := h.platform.ProbeDatabaseConnection(r.Context(), p, target, name)
		respondPlatform(w, value, err)
		return
	}
	methodNotAllowed(w)
}

func optionalVersion(w http.ResponseWriter, r *http.Request) (*int64, bool) {
	raw := r.URL.Query().Get("ifVersion")
	if raw == "" {
		return nil, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 0 {
		writeError(w, http.StatusBadRequest, "invalid_version")
		return nil, false
	}
	return &value, true
}

func requirePlatformRole(w http.ResponseWriter, p portalapi.Principal, role string) bool {
	if p.System || hasRole(p.Roles, "platform.admin") || hasRole(p.Roles, role) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden")
	return false
}

func requireManagementOperation(w http.ResponseWriter, target portalapi.ManagementTarget, capability, operation string, write bool) bool {
	if target.Allows(capability, operation, write) {
		return true
	}
	writeError(w, http.StatusForbidden, "management_binding_forbidden")
	return false
}

func hasRole(roles []string, role string) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}

func respondPlatform(w http.ResponseWriter, value any, err error) {
	if err == nil {
		writeJSON(w, http.StatusOK, value)
		return
	}
	var capabilityErr *CapabilityError
	if errors.As(err, &capabilityErr) {
		switch capabilityErr.Code {
		case errorcode.PermissionDenied:
			writeError(w, http.StatusForbidden, "forbidden")
		case "platform.settings.not_found", "platform.credentials.not_found", "platform.database.not_found", "platform.deployment.not_found":
			writeError(w, http.StatusNotFound, "not_found")
		case "platform.settings.version_conflict", "platform.deployment.version_conflict":
			writeError(w, http.StatusConflict, "version_conflict")
		case "platform.deployment.separation_required":
			writeError(w, http.StatusConflict, "separation_required")
		case "platform.deployment.job_conflict":
			writeError(w, http.StatusConflict, "job_conflict")
		case "platform.deployment.service_state_conflict":
			writeError(w, http.StatusConflict, "service_state_conflict")
		case "platform.settings.invalid", "platform.credentials.invalid", "platform.database.invalid", "platform.deployment.invalid":
			writeError(w, http.StatusBadRequest, "invalid_request")
		case "platform.deployment.bootstrap_failed":
			writeError(w, http.StatusBadGateway, "bootstrap_failed")
		case "platform.deployment.service_publish_failed":
			writeError(w, http.StatusBadGateway, "service_publish_failed")
		default:
			writeError(w, http.StatusBadGateway, "platform_service_unavailable")
		}
		return
	}
	switch {
	case errors.Is(err, portalapi.ErrForbidden):
		writeError(w, http.StatusForbidden, "management_binding_forbidden")
	case errors.Is(err, platformadminapi.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found")
	case errors.Is(err, platformadminapi.ErrConflict):
		writeError(w, http.StatusConflict, "version_conflict")
	case errors.Is(err, platformadminapi.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request")
	default:
		writeError(w, http.StatusBadGateway, "platform_service_unavailable")
	}
}
