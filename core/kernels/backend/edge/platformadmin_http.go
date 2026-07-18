package edge

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

func (h *Handler) platformRoute(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if h.platform == nil {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/platform/"), "/")
	if len(parts) == 1 {
		switch parts[0] {
		case "settings":
			h.listSettings(w, r, p)
			return
		case "credentials":
			h.listCredentials(w, r, p)
			return
		case "database-connections":
			h.listDatabaseConnections(w, r, p)
			return
		}
	}
	if len(parts) == 2 && parts[0] == "artifacts" && parts[1] == "status" {
		if !requirePlatformRole(w, p, "platform.artifacts.read") {
			return
		}
		if r.Method != http.MethodGet {
			methodNotAllowed(w)
			return
		}
		value, err := h.platform.ArtifactRepositoryStatus(r.Context(), p)
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
		h.settingItem(w, r, p, name, parts)
	case "credentials":
		h.credentialItem(w, r, p, name, parts)
	case "database-connections":
		h.databaseConnectionItem(w, r, p, name, parts)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) listSettings(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if !requirePlatformRole(w, p, "platform.settings.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListSettings(r.Context(), p, r.URL.Query().Get("prefix"))
	respondPlatform(w, value, err)
}

func (h *Handler) settingItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, key string, parts []string) {
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	if !requirePlatformRole(w, p, "platform.admin") {
		return
	}
	switch r.Method {
	case http.MethodPut:
		var request platformadminapi.PutSettingRequest
		if !decode(w, r, &request) {
			return
		}
		value, err := h.platform.PutSetting(r.Context(), p, key, request)
		respondPlatform(w, value, err)
	case http.MethodDelete:
		version, ok := optionalVersion(w, r)
		if !ok {
			return
		}
		respondPlatform(w, map[string]bool{"deleted": true}, h.platform.DeleteSetting(r.Context(), p, key, version))
	default:
		methodNotAllowed(w)
	}
}

func (h *Handler) listCredentials(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if !requirePlatformRole(w, p, "platform.credentials.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListCredentials(r.Context(), p, r.URL.Query().Get("prefix"))
	respondPlatform(w, value, err)
}

func (h *Handler) credentialItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, name string, parts []string) {
	if len(parts) == 2 && r.Method == http.MethodPut {
		if !requirePlatformRole(w, p, "platform.credentials.write") {
			return
		}
		var request platformadminapi.PutCredentialRequest
		if !decode(w, r, &request) {
			return
		}
		value, err := h.platform.PutCredential(r.Context(), p, name, request)
		respondPlatform(w, value, err)
		return
	}
	if len(parts) == 3 && r.Method == http.MethodPost {
		var value platformadminapi.CredentialMetadata
		var err error
		switch parts[2] {
		case "rotate":
			if !requirePlatformRole(w, p, "platform.credentials.rotate") {
				return
			}
			value, err = h.platform.RotateCredential(r.Context(), p, name)
		case "revoke":
			if !requirePlatformRole(w, p, "platform.credentials.revoke") {
				return
			}
			value, err = h.platform.RevokeCredential(r.Context(), p, name)
		default:
			http.NotFound(w, r)
			return
		}
		respondPlatform(w, value, err)
		return
	}
	methodNotAllowed(w)
}

func (h *Handler) listDatabaseConnections(w http.ResponseWriter, r *http.Request, p portalapi.Principal) {
	if !requirePlatformRole(w, p, "platform.database.read") {
		return
	}
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	value, err := h.platform.ListDatabaseConnections(r.Context(), p)
	respondPlatform(w, value, err)
}

func (h *Handler) databaseConnectionItem(w http.ResponseWriter, r *http.Request, p portalapi.Principal, name string, parts []string) {
	if len(parts) == 2 {
		if !requirePlatformRole(w, p, "platform.database.write") {
			return
		}
		switch r.Method {
		case http.MethodPut:
			var request platformadminapi.DatabaseConnection
			if !decode(w, r, &request) {
				return
			}
			value, err := h.platform.PutDatabaseConnection(r.Context(), p, name, request)
			respondPlatform(w, value, err)
		case http.MethodDelete:
			respondPlatform(w, map[string]bool{"deleted": true}, h.platform.DeleteDatabaseConnection(r.Context(), p, name))
		default:
			methodNotAllowed(w)
		}
		return
	}
	if len(parts) == 3 && parts[2] == "probe" && r.Method == http.MethodPost {
		if !requirePlatformRole(w, p, "platform.database.probe") {
			return
		}
		value, err := h.platform.ProbeDatabaseConnection(r.Context(), p, name)
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
		case "platform.settings.not_found", "platform.credentials.not_found", "platform.database.not_found":
			writeError(w, http.StatusNotFound, "not_found")
		case "platform.settings.version_conflict":
			writeError(w, http.StatusConflict, "version_conflict")
		case "platform.settings.invalid", "platform.credentials.invalid", "platform.database.invalid":
			writeError(w, http.StatusBadRequest, "invalid_request")
		default:
			writeError(w, http.StatusBadGateway, "platform_service_unavailable")
		}
		return
	}
	switch {
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
