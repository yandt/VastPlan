package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// runtime proxies the authenticated canonical RuntimeSpec, then overlays only
// the current development generation. Page and identity handling remain owned
// by Portal Host; HMR never becomes a parallel application server.
func (h *frontendHMR) runtime(w http.ResponseWriter, request *http.Request) {
	if !loopbackRequest(request) || request.Method != http.MethodGet {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	query := request.URL.Query()
	if strings.TrimSpace(query.Get("path")) == "" {
		query.Set("path", "/operations")
	}
	target := h.portalURL + "/v1/portal-runtime?" + query.Encode()
	upstream, err := http.NewRequestWithContext(request.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.identity != nil {
		h.identity.decorateUpstream(request, upstream)
	} else {
		copyIdentityCookies(request, upstream)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(upstream)
	if err != nil {
		http.Error(w, "Portal Runtime upstream unavailable", http.StatusBadGateway)
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil || response.StatusCode != http.StatusOK {
		http.Error(w, "Portal Runtime upstream rejected request", response.StatusCode)
		return
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal(body, &document); err != nil {
		http.Error(w, "Portal Runtime upstream invalid", http.StatusBadGateway)
		return
	}
	if err := h.overlayRuntime(document); err != nil {
		http.Error(w, err.message, err.status)
		return
	}
	encodedDocument, err := json.Marshal(document)
	if err != nil {
		http.Error(w, "Portal Runtime overlay invalid", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(encodedDocument)
}

type frontendHMRRuntimeError struct {
	status  int
	message string
}

func (h *frontendHMR) overlayRuntime(document map[string]json.RawMessage) *frontendHMRRuntimeError {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if err := overlayFrontendHMRUIContracts(document, h.current); err != nil {
		return &frontendHMRRuntimeError{http.StatusConflict, "Portal Runtime UI contract overlay invalid: " + err.Error()}
	}
	if err := overlayRuntimeModules(document, h.current); err != nil {
		return err
	}
	if err := overlayRuntimeGraphs(document, h.current); err != nil {
		return err
	}
	if err := overlayFrontendHMRContributions(document, h.current); err != nil {
		return &frontendHMRRuntimeError{http.StatusConflict, "Portal Runtime contribution overlay invalid: " + err.Error()}
	}
	return nil
}

func overlayRuntimeModules(document map[string]json.RawMessage, current map[string]frontendHMRModule) *frontendHMRRuntimeError {
	raw, exists := document["modules"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var descriptors []map[string]any
	if json.Unmarshal(raw, &descriptors) != nil {
		return &frontendHMRRuntimeError{http.StatusBadGateway, "Portal Runtime upstream invalid"}
	}
	for _, descriptor := range descriptors {
		id, _ := descriptor["id"].(string)
		if module, ok := current[id]; ok {
			descriptor["entry"], descriptor["url"], descriptor["sha256"], descriptor["deferred"] = module.Entry, "/__vastplan_dev/modules/"+module.SHA256+".js", module.SHA256, module.Deferred
		}
	}
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		return &frontendHMRRuntimeError{http.StatusInternalServerError, "Portal Runtime overlay invalid"}
	}
	document["modules"] = encoded
	return nil
}

func overlayRuntimeGraphs(document map[string]json.RawMessage, current map[string]frontendHMRModule) *frontendHMRRuntimeError {
	raw, exists := document["moduleGraphs"]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var descriptors []map[string]any
	if json.Unmarshal(raw, &descriptors) != nil {
		return &frontendHMRRuntimeError{http.StatusBadGateway, "Portal Runtime upstream invalid"}
	}
	for _, descriptor := range descriptors {
		id, _ := descriptor["id"].(string)
		module, ok := current[id]
		if !ok || module.Graph == nil {
			continue
		}
		descriptor["target"], descriptor["entry"], descriptor["digest"] = module.Graph.Target, module.Graph.Entry, module.Graph.Digest
		descriptor["externals"], descriptor["nodes"] = module.Graph.Externals, module.Graph.Nodes
		if module.Deferred {
			descriptor["deferred"] = true
		} else {
			delete(descriptor, "deferred")
		}
	}
	encoded, err := json.Marshal(descriptors)
	if err != nil {
		return &frontendHMRRuntimeError{http.StatusInternalServerError, "Portal Runtime overlay invalid"}
	}
	document["moduleGraphs"] = encoded
	return nil
}
