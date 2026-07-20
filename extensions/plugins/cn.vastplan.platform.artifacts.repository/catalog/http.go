package catalog

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

type HTTPHandler struct {
	Store      *Store
	ReadToken  string
	RequireTLS bool
	Logf       func(string, ...any)
}

func (h *HTTPHandler) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if h.Store == nil {
		http.Error(w, "catalog unavailable", http.StatusServiceUnavailable)
		return
	}
	if h.RequireTLS && request.TLS == nil {
		http.Error(w, "TLS required", http.StatusUpgradeRequired)
		return
	}
	if request.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !catalogAuthorized(request, h.ReadToken) {
		h.log("catalog.read_denied remote=%s", request.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var response any
	var err error
	switch request.URL.Path {
	case "/v1/catalog/artifacts":
		response, err = h.catalog(request.URL.Query())
	case "/v1/catalog/journal":
		response, err = h.journal(request.URL.Query())
	default:
		http.NotFound(w, request)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *HTTPHandler) catalog(values url.Values) (Page, error) {
	if err := allowQuery(values, "pluginId", "pluginPrefix", "namespace", "publisher", "version", "channel", "target", "page", "pageSize"); err != nil {
		return Page{}, err
	}
	page, err := positiveInt(values.Get("page"), 1, 1_000_000)
	if err != nil {
		return Page{}, fmt.Errorf("page: %w", err)
	}
	pageSize, err := positiveInt(values.Get("pageSize"), 20, 100)
	if err != nil {
		return Page{}, fmt.Errorf("pageSize: %w", err)
	}
	return h.Store.Query(Query{
		PluginID: values.Get("pluginId"), PluginPrefix: values.Get("pluginPrefix"), Namespace: values.Get("namespace"),
		Publisher: values.Get("publisher"), Version: values.Get("version"), Channel: values.Get("channel"),
		Target: values.Get("target"), Page: page, PageSize: pageSize,
	}), nil
}

func (h *HTTPHandler) journal(values url.Values) (JournalPage, error) {
	if err := allowQuery(values, "afterRevision", "limit"); err != nil {
		return JournalPage{}, err
	}
	after, err := nonnegativeUint(values.Get("afterRevision"))
	if err != nil {
		return JournalPage{}, fmt.Errorf("afterRevision: %w", err)
	}
	limit, err := positiveInt(values.Get("limit"), 50, 200)
	if err != nil {
		return JournalPage{}, fmt.Errorf("limit: %w", err)
	}
	return h.Store.Journal(after, limit), nil
}

func allowQuery(values url.Values, allowed ...string) error {
	set := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		set[key] = struct{}{}
	}
	for key, items := range values {
		if _, ok := set[key]; !ok {
			return fmt.Errorf("unknown query parameter %q", key)
		}
		if len(items) != 1 {
			return fmt.Errorf("query parameter %q must appear once", key)
		}
	}
	return nil
}

func positiveInt(raw string, fallback, maximum int) (int, error) {
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 || value > maximum {
		return 0, fmt.Errorf("must be between 1 and %d", maximum)
	}
	return value, nil
}

func nonnegativeUint(raw string) (uint64, error) {
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, errors.New("must be a non-negative integer")
	}
	return value, nil
}

func catalogAuthorized(request *http.Request, token string) bool {
	want, got := "Bearer "+token, request.Header.Get("Authorization")
	return token != "" && len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (h *HTTPHandler) log(format string, args ...any) {
	if h.Logf != nil {
		h.Logf(format, args...)
	}
}
