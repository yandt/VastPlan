package catalog

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

const maxResolveRequestBytes = int64(256 << 10)

type HTTPHandler struct {
	Store             Service
	ReadToken         string
	BundleToken       string
	ImportToken       string
	AssessmentToken   string
	BundleSource      OfflineBundleSource
	BundleDestination OfflineBundleDestination
	TrustSnapshot     []byte
	BundleDirectory   string
	RequireTLS        bool
	Logf              func(string, ...any)
}

func requireExactQuery(values url.Values, required ...string) error {
	if err := allowQuery(values, required...); err != nil {
		return err
	}
	for _, key := range required {
		items := values[key]
		if len(items) != 1 || items[0] == "" {
			return fmt.Errorf("query parameter %q is required exactly once", key)
		}
	}
	return nil
}

// Service is the catalog read surface. A repository migration coordinator can
// atomically redirect it to a verified candidate without exposing storage paths
// to the HTTP layer.
type Service interface {
	Query(Query) Page
	Journal(uint64, int) JournalPage
	Resolve(pluginv1.ArtifactResolveRequest) (pluginv1.ArtifactLock, error)
}

type SecurityStatusService interface {
	AppendSecurityStatus(pluginv1.ArtifactRef, []byte, time.Time) (*artifactassessment.StatusRecord, string, error)
	ReadSecurityStatusChain(pluginv1.ArtifactRef) ([]byte, error)
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
	token := h.ReadToken
	if request.URL.Path == "/v1/catalog/bundles" {
		token = h.BundleToken
	} else if request.URL.Path == "/v1/catalog/bundles/import" {
		token = h.ImportToken
	} else if request.URL.Path == "/v1/catalog/security-status" && request.Method == http.MethodPost {
		token = h.AssessmentToken
	}
	if !catalogAuthorized(request, token) {
		h.log("catalog.read_denied remote=%s", request.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var response any
	var err error
	switch request.URL.Path {
	case "/v1/catalog/artifacts":
		if !requireMethod(w, request, http.MethodGet) {
			return
		}
		response, err = h.catalog(request.URL.Query())
	case "/v1/catalog/journal":
		if !requireMethod(w, request, http.MethodGet) {
			return
		}
		response, err = h.journal(request.URL.Query())
	case "/v1/catalog/resolve":
		if !requireMethod(w, request, http.MethodPost) {
			return
		}
		response, err = h.resolve(w, request)
	case "/v1/catalog/bundles":
		if !requireMethod(w, request, http.MethodPost) {
			return
		}
		h.bundle(w, request)
		return
	case "/v1/catalog/bundles/import":
		if !requireMethod(w, request, http.MethodPost) {
			return
		}
		h.importBundle(w, request)
		return
	case "/v1/catalog/security-status":
		h.securityStatus(w, request)
		return
	default:
		http.NotFound(w, request)
		return
	}
	if err != nil {
		var resolution *ResolutionError
		if errors.As(err, &resolution) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(resolution)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}

func (h *HTTPHandler) securityStatus(w http.ResponseWriter, request *http.Request) {
	service, ok := h.Store.(SecurityStatusService)
	if !ok {
		http.Error(w, "security status unavailable", http.StatusServiceUnavailable)
		return
	}
	if request.Method == http.MethodGet {
		query := request.URL.Query()
		if err := requireExactQuery(query, "pluginId", "version", "channel"); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ref := pluginv1.ArtifactRef{PluginID: query.Get("pluginId"), Version: query.Get("version"), Channel: query.Get("channel")}
		raw, err := service.ReadSecurityStatusChain(ref)
		if err != nil || len(raw) == 0 {
			http.NotFound(w, request)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
		return
	}
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if len(request.URL.Query()) != 0 {
		http.Error(w, "security status publish does not accept query", http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, artifactassessment.MaxRecordBytes+(16<<10))
	var payload struct {
		Ref    pluginv1.ArtifactRef `json:"ref"`
		Record json.RawMessage      `json:"record"`
	}
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.Ref.PluginID == "" || payload.Ref.Version == "" || payload.Ref.Channel == "" || len(payload.Record) == 0 {
		http.Error(w, "invalid security status", http.StatusBadRequest)
		return
	}
	record, digest, err := service.AppendSecurityStatus(payload.Ref, payload.Record, time.Now().UTC())
	if err != nil {
		h.log("security_status.publish_rejected ref=%s@%s/%s error=%v", payload.Ref.PluginID, payload.Ref.Version, payload.Ref.Channel, err)
		http.Error(w, "security status rejected", http.StatusUnprocessableEntity)
		return
	}
	h.log("security_status.published ref=%s@%s/%s sequence=%d digest=%s decision=%s", payload.Ref.PluginID, payload.Ref.Version, payload.Ref.Channel, record.Sequence, digest, record.Evaluation.Decision)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{"ref": payload.Ref, "sequence": record.Sequence, "digest": digest, "decision": record.Evaluation.Decision})
}

func (h *HTTPHandler) importBundle(w http.ResponseWriter, request *http.Request) {
	if h.BundleDestination == nil || h.BundleDirectory == "" {
		http.Error(w, "bundle import unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(request.URL.Query()) != 0 {
		http.Error(w, "bundle import endpoint does not accept query parameters", http.StatusBadRequest)
		return
	}
	if err := ensurePrivateDirectory(h.BundleDirectory); err != nil {
		http.Error(w, "bundle import unavailable", http.StatusInternalServerError)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxOfflineBundleBytes+(32<<20))
	file, err := os.CreateTemp(h.BundleDirectory, ".bundle-upload-*.tar.gz")
	if err != nil {
		http.Error(w, "bundle import unavailable", http.StatusInternalServerError)
		return
	}
	path := file.Name()
	defer os.Remove(path)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		http.Error(w, "bundle import unavailable", http.StatusInternalServerError)
		return
	}
	_, copyErr := io.Copy(file, request.Body)
	syncErr, closeErr := file.Sync(), file.Close()
	if err := errors.Join(copyErr, syncErr, closeErr); err != nil {
		http.Error(w, "bundle upload rejected", http.StatusRequestEntityTooLarge)
		return
	}
	lock, err := ImportOfflineBundle(path, h.BundleDestination)
	if err != nil {
		h.log("bundle.import_rejected error=%v", err)
		http.Error(w, "bundle import rejected", http.StatusUnprocessableEntity)
		return
	}
	h.log("bundle.imported lock=%s packages=%d", lock.Digest, len(lock.Packages))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(lock)
}

func (h *HTTPHandler) bundle(w http.ResponseWriter, request *http.Request) {
	if h.BundleSource == nil || len(h.TrustSnapshot) == 0 || h.BundleDirectory == "" {
		http.Error(w, "bundle unavailable", http.StatusServiceUnavailable)
		return
	}
	if len(request.URL.Query()) != 0 {
		http.Error(w, "bundle endpoint does not accept query parameters", http.StatusBadRequest)
		return
	}
	request.Body = http.MaxBytesReader(w, request.Body, 2<<20)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		http.Error(w, "bundle lock exceeds 2MiB or cannot be read", http.StatusBadRequest)
		return
	}
	var lock pluginv1.ArtifactLock
	if err := decodeStrict(raw, &lock); err != nil {
		http.Error(w, "invalid artifact lock", http.StatusBadRequest)
		return
	}
	bundle, err := CreateOfflineBundle(lock, h.TrustSnapshot, h.BundleSource, h.BundleDirectory)
	if err != nil {
		h.log("bundle.build_rejected lock=%s error=%v", lock.Digest, err)
		http.Error(w, "bundle rejected", http.StatusUnprocessableEntity)
		return
	}
	defer os.Remove(bundle.Path)
	file, err := os.Open(bundle.Path)
	if err != nil {
		http.Error(w, "bundle unavailable", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vastplan-%s.tar.gz"`, lock.Digest[:16]))
	w.Header().Set("Content-Length", strconv.FormatInt(bundle.Size, 10))
	w.Header().Set("ETag", `"sha256-`+bundle.SHA256+`"`)
	w.Header().Set("X-VastPlan-Bundle-SHA256", bundle.SHA256)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
	h.log("bundle.built lock=%s sha256=%s size=%d", lock.Digest, bundle.SHA256, bundle.Size)
}

func (h *HTTPHandler) resolve(w http.ResponseWriter, request *http.Request) (pluginv1.ArtifactLock, error) {
	if len(request.URL.Query()) != 0 {
		return pluginv1.ArtifactLock{}, errors.New("resolve endpoint does not accept query parameters")
	}
	request.Body = http.MaxBytesReader(w, request.Body, maxResolveRequestBytes)
	raw, err := io.ReadAll(request.Body)
	if err != nil {
		return pluginv1.ArtifactLock{}, errors.New("resolve request exceeds 256KiB or cannot be read")
	}
	var input pluginv1.ArtifactResolveRequest
	if err := decodeStrict(raw, &input); err != nil {
		return pluginv1.ArtifactLock{}, fmt.Errorf("invalid resolve request: %w", err)
	}
	return h.Store.Resolve(input)
}

func requireMethod(w http.ResponseWriter, request *http.Request, method string) bool {
	if request.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
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
