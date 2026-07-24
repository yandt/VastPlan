package localtest

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

type Server struct {
	profile    artifactrepositoryv1.Profile
	repository Repository
	token      string
}

func NewServer(profile artifactrepositoryv1.Profile, repository Repository, token string) (*Server, error) {
	profile, err := validateBinding(profile, token)
	if err != nil {
		return nil, err
	}
	if repository == nil {
		return nil, errors.New("local-test Repository 不能为空")
	}
	if profile.Workspace != nil {
		if _, ok := repository.(WorkspaceRepository); !ok {
			return nil, errors.New("启用 workspace 的 local-test Repository 必须实现过期端口")
		}
	}
	return &Server{profile: profile, repository: repository, token: token}, nil
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if req.Header.Get(ProtocolHeader) != artifactrepositoryv1.ProtocolLocalTest {
		http.Error(w, "repository protocol mismatch", http.StatusPreconditionFailed)
		return
	}
	if !authorized(req, s.token) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if len(req.URL.Query()) != 0 {
		http.Error(w, "query not allowed", http.StatusBadRequest)
		return
	}
	switch {
	case req.Method == http.MethodPost && req.URL.Path == "/v1/artifacts":
		s.publish(w, req)
	case req.Method == http.MethodGet && req.URL.Path == "/v1/catalog":
		s.catalog(w, req)
	case req.Method == http.MethodPost && req.URL.Path == "/v1/workspace/expire":
		s.expireWorkspace(w, req)
	case req.Method == http.MethodGet && strings.HasPrefix(req.URL.Path, "/v1/artifacts/"):
		s.read(w, req)
	default:
		http.NotFound(w, req)
	}
}

func (s *Server) publish(w http.ResponseWriter, req *http.Request) {
	limited := http.MaxBytesReader(w, req.Body, maxRequestBytes())
	envelope, err := readEnvelope(limited, req.Header.Get("Content-Type"))
	if err != nil {
		http.Error(w, "invalid artifact envelope", http.StatusBadRequest)
		return
	}
	if err := validateEnvelopeForProfile(s.profile, envelope); err != nil {
		http.Error(w, "artifact rejected by repository profile", http.StatusUnprocessableEntity)
		return
	}
	receipt, err := s.repository.Publish(req.Context(), envelope)
	if err != nil {
		http.Error(w, "artifact rejected", http.StatusUnprocessableEntity)
		return
	}
	if err := artifactrepositoryv1.ValidateReceipt(s.profile, receipt); err != nil || !sameRef(receipt.Ref, exactRef(envelope)) || receipt.SHA256 != envelope.Artifact.SHA256 {
		http.Error(w, "repository returned invalid receipt", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusCreated, receipt)
}

func (s *Server) read(w http.ResponseWriter, req *http.Request) {
	ref, err := refFromPath(req.URL)
	if err != nil {
		http.Error(w, "invalid artifact reference", http.StatusBadRequest)
		return
	}
	if err := artifactrepositoryv1.ValidateRef(s.profile, ref); err != nil {
		http.Error(w, "reference not allowed by repository profile", http.StatusBadRequest)
		return
	}
	envelope, err := s.repository.ReadExact(req.Context(), ref)
	if err != nil {
		if errors.Is(err, artifacttrust.ErrNotFound) {
			http.NotFound(w, req)
			return
		}
		http.Error(w, "repository unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := validateEnvelopeForProfile(s.profile, envelope); err != nil || !sameRef(exactRef(envelope), ref) {
		http.Error(w, "repository returned invalid envelope", http.StatusBadGateway)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	writer := multipart.NewWriter(w)
	w.Header().Set("Content-Type", writer.FormDataContentType())
	if err := writeEnvelope(writer, envelope); err != nil {
		_ = writer.Close()
		return
	}
	_ = writer.Close()
}

func (s *Server) catalog(w http.ResponseWriter, req *http.Request) {
	snapshot, err := s.repository.CatalogSnapshot(req.Context())
	if err != nil {
		http.Error(w, "repository unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := artifactrepositoryv1.ValidateCatalogSnapshot(s.profile, snapshot); err != nil {
		http.Error(w, "repository returned invalid catalog", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) expireWorkspace(w http.ResponseWriter, req *http.Request) {
	if s.profile.Workspace == nil {
		http.NotFound(w, req)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, 1025))
	if err != nil || len(raw) > 1024 {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if len(raw) > 0 {
		var value struct{}
		decoder := json.NewDecoder(strings.NewReader(string(raw)))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&value); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
	}
	repository, ok := s.repository.(WorkspaceRepository)
	if !ok {
		http.NotFound(w, req)
		return
	}
	result, err := repository.ExpireWorkspace(req.Context())
	if err != nil {
		http.Error(w, "workspace expiration failed", http.StatusServiceUnavailable)
		return
	}
	if err := artifactrepositoryv1.ValidateExpireWorkspaceResult(s.profile, result); err != nil {
		http.Error(w, "repository returned invalid workspace result", http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func refFromPath(value *url.URL) (pluginv1.ArtifactRef, error) {
	parts := strings.Split(strings.Trim(value.EscapedPath(), "/"), "/")
	if len(parts) != 5 || parts[0] != "v1" || parts[1] != "artifacts" {
		return pluginv1.ArtifactRef{}, errors.New("path shape invalid")
	}
	pluginID, err1 := url.PathUnescape(parts[2])
	version, err2 := url.PathUnescape(parts[3])
	channel, err3 := url.PathUnescape(parts[4])
	if err1 != nil || err2 != nil || err3 != nil || pluginID == "" || version == "" || channel == "" || strings.ContainsAny(pluginID+version+channel, "/\x00") {
		return pluginv1.ArtifactRef{}, errors.New("path value invalid")
	}
	return pluginv1.ArtifactRef{PluginID: pluginID, Version: version, Channel: channel}, nil
}

func authorized(req *http.Request, token string) bool {
	header := req.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		return false
	}
	provided := strings.TrimPrefix(header, "Bearer ")
	if len(provided) != len(token) || len(provided) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

func artifactPath(ref pluginv1.ArtifactRef) string {
	return fmt.Sprintf("/v1/artifacts/%s/%s/%s", url.PathEscape(ref.PluginID), url.PathEscape(ref.Version), url.PathEscape(ref.Channel))
}
