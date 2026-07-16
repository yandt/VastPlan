package pluginservice

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultMaxArtifactBytes = int64(256 << 20)
	maxAttestationBytes     = int64(2 << 20)
)

// RemoteRepository 从 HTTPS 制品服务读取签名证明和对象，并在客户端再次执行信任链、
// SHA-256、大小和清单绑定检查。服务端被入侵也不能绕过 Node Agent 的本地信任根。
type RemoteRepository struct {
	BaseURL   string
	Token     string
	Trust     *TrustStore
	Client    *http.Client
	MaxBytes  int64
	AllowHTTP bool // 仅供本地开发和 httptest；生产配置保持 false。
}

func (r *RemoteRepository) Read(ref Ref) (Artifact, []byte, error) {
	base, client, maxBytes, err := r.validate()
	if err != nil {
		return Artifact{}, nil, err
	}
	endpoint := artifactEndpoint(base, ref)
	attestationRaw, err := r.get(client, endpoint+"/attestation", maxAttestationBytes)
	if err != nil {
		return Artifact{}, nil, err
	}
	var attestation Attestation
	if err := decodeJSONStrict(attestationRaw, &attestation); err != nil {
		return Artifact{}, nil, fmt.Errorf("解析远端制品证明: %w", err)
	}
	artifact := attestation.Artifact
	if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel {
		return Artifact{}, nil, errors.New("远端制品证明与请求引用不一致")
	}
	if err := r.Trust.Verify(attestation); err != nil {
		return Artifact{}, nil, fmt.Errorf("远端制品证明不可信: %w", err)
	}
	if artifact.Size > maxBytes {
		return Artifact{}, nil, fmt.Errorf("远端制品大小 %d 超过客户端上限 %d", artifact.Size, maxBytes)
	}
	packageBytes, err := r.get(client, endpoint+"/package", maxBytes)
	if err != nil {
		return Artifact{}, nil, err
	}
	if err := ValidateArtifact(artifact, packageBytes); err != nil {
		return Artifact{}, nil, fmt.Errorf("远端制品内容校验失败: %w", err)
	}
	return artifact, packageBytes, nil
}

func (r *RemoteRepository) validate() (*url.URL, *http.Client, int64, error) {
	if r == nil || r.Trust == nil {
		return nil, nil, 0, errors.New("远端制品仓库及信任根必须配置")
	}
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(r.BaseURL), "/"))
	if err != nil || base.Host == "" || (base.Scheme != "https" && (!r.AllowHTTP || base.Scheme != "http")) {
		return nil, nil, 0, errors.New("远端制品仓库必须使用合法 HTTPS URL")
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	maxBytes := r.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxArtifactBytes
	}
	return base, client, maxBytes, nil
}

func (r *RemoteRepository) get(client *http.Client, endpoint string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	setBearer(req, r.Token)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("读取远端制品 %s: %w", endpoint, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("远端制品服务返回 %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("远端响应超过 %d 字节上限", limit)
	}
	return raw, nil
}

// PublishRemote 通过 multipart 上传签名证明和制品，不把制品再 base64 膨胀一遍。
func (r *RemoteRepository) PublishRemote(ctx context.Context, attestation Attestation, packageBytes []byte) (Artifact, error) {
	base, client, maxBytes, err := r.validate()
	if err != nil {
		return Artifact{}, err
	}
	if int64(len(packageBytes)) > maxBytes {
		return Artifact{}, fmt.Errorf("制品超过客户端上限 %d", maxBytes)
	}
	if err := ValidateArtifact(attestation.Artifact, packageBytes); err != nil {
		return Artifact{}, err
	}
	if err := r.Trust.Verify(attestation); err != nil {
		return Artifact{}, err
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	attestationPart, err := writer.CreateFormField("attestation")
	if err != nil {
		return Artifact{}, err
	}
	if err := json.NewEncoder(attestationPart).Encode(attestation); err != nil {
		return Artifact{}, err
	}
	packagePart, err := writer.CreateFormFile("package", attestation.Artifact.Object)
	if err != nil {
		return Artifact{}, err
	}
	if _, err := packagePart.Write(packageBytes); err != nil {
		return Artifact{}, err
	}
	if err := writer.Close(); err != nil {
		return Artifact{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String()+"/v1/artifacts", &body)
	if err != nil {
		return Artifact{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	setBearer(req, r.Token)
	resp, err := client.Do(req)
	if err != nil {
		return Artifact{}, fmt.Errorf("发布远端制品: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return Artifact{}, fmt.Errorf("远端发布返回 %s: %s", resp.Status, strings.TrimSpace(string(message)))
	}
	var artifact Artifact
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxAttestationBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&artifact); err != nil {
		return Artifact{}, fmt.Errorf("解析远端发布响应: %w", err)
	}
	if !sameArtifact(artifact, attestation.Artifact) {
		return Artifact{}, errors.New("远端发布响应与签名制品不一致")
	}
	return artifact, nil
}

func artifactEndpoint(base *url.URL, ref Ref) string {
	return base.String() + "/v1/artifacts/" + url.PathEscape(ref.PluginID) + "/" +
		url.PathEscape(ref.Version) + "/" + url.PathEscape(ref.Channel)
}

func setBearer(req *http.Request, token string) {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}

// ArtifactHTTPServer 暴露最小远端制品 API。读写令牌分离；RequireTLS 默认应为 true，
// 只有进程内测试或明确的本地开发反向代理场景才关闭。
type ArtifactHTTPServer struct {
	Repository   *SignedRepository
	ReadToken    string
	PublishToken string
	RequireTLS   bool
	MaxBytes     int64
	Logf         func(string, ...any)
}

func (s *ArtifactHTTPServer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if s.Repository == nil || s.Repository.Local == nil || s.Repository.Trust == nil {
		http.Error(w, "repository unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.RequireTLS && req.TLS == nil {
		http.Error(w, "TLS required", http.StatusUpgradeRequired)
		return
	}
	if req.Method == http.MethodPost && req.URL.Path == "/v1/artifacts" {
		s.publish(w, req)
		return
	}
	if req.Method == http.MethodGet {
		s.read(w, req)
		return
	}
	w.Header().Set("Allow", "GET, POST")
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func (s *ArtifactHTTPServer) publish(w http.ResponseWriter, req *http.Request) {
	if !authorized(req, s.PublishToken) {
		s.log("artifact.publish_denied remote=%s", req.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	maxBytes := s.maxBytes()
	req.Body = http.MaxBytesReader(w, req.Body, maxBytes+maxAttestationBytes+(1<<20))
	reader, err := req.MultipartReader()
	if err != nil {
		http.Error(w, "multipart required", http.StatusBadRequest)
		return
	}
	var attestationRaw, packageBytes []byte
	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			http.Error(w, "invalid multipart", http.StatusBadRequest)
			return
		}
		switch part.FormName() {
		case "attestation":
			attestationRaw, err = readLimited(part, maxAttestationBytes)
		case "package":
			packageBytes, err = readLimited(part, maxBytes)
		}
		_ = part.Close()
		if err != nil {
			http.Error(w, err.Error(), http.StatusRequestEntityTooLarge)
			return
		}
	}
	if len(attestationRaw) == 0 || len(packageBytes) == 0 {
		http.Error(w, "attestation and package are required", http.StatusBadRequest)
		return
	}
	var attestation Attestation
	if err := decodeJSONStrict(attestationRaw, &attestation); err != nil {
		http.Error(w, "invalid attestation", http.StatusBadRequest)
		return
	}
	artifact, err := s.Repository.Publish(attestation, packageBytes)
	if err != nil {
		s.log("artifact.publish_rejected plugin=%s version=%s key=%s error=%v", attestation.Artifact.PluginID, attestation.Artifact.Version, attestation.KeyID, err)
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	s.log("artifact.published plugin=%s version=%s channel=%s sha256=%s publisher=%s key=%s", artifact.PluginID, artifact.Version, artifact.Channel, artifact.SHA256, attestation.Publisher, attestation.KeyID)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(artifact)
}

func (s *ArtifactHTTPServer) read(w http.ResponseWriter, req *http.Request) {
	if !authorized(req, s.ReadToken) {
		s.log("artifact.read_denied remote=%s", req.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	parts := strings.Split(strings.Trim(req.URL.EscapedPath(), "/"), "/")
	if len(parts) != 6 || parts[0] != "v1" || parts[1] != "artifacts" {
		http.NotFound(w, req)
		return
	}
	pluginID, err1 := url.PathUnescape(parts[2])
	version, err2 := url.PathUnescape(parts[3])
	channel, err3 := url.PathUnescape(parts[4])
	if err1 != nil || err2 != nil || err3 != nil {
		http.Error(w, "invalid artifact reference", http.StatusBadRequest)
		return
	}
	ref := Ref{PluginID: pluginID, Version: version, Channel: channel}
	artifact, packageBytes, err := s.Repository.Read(ref)
	if err != nil {
		http.Error(w, "artifact not found or untrusted", http.StatusNotFound)
		return
	}
	switch parts[5] {
	case "package":
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(packageBytes)))
		_, _ = w.Write(packageBytes)
	case "attestation":
		dir, _ := s.Repository.Local.artifactDir(ref)
		raw, readErr := os.ReadFile(filepath.Join(dir, "attestation.json"))
		if readErr != nil {
			http.Error(w, "attestation unavailable", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	default:
		http.NotFound(w, req)
		return
	}
	s.log("artifact.read plugin=%s version=%s channel=%s sha256=%s", artifact.PluginID, artifact.Version, artifact.Channel, artifact.SHA256)
}

func (s *ArtifactHTTPServer) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return defaultMaxArtifactBytes
}

func (s *ArtifactHTTPServer) log(format string, values ...any) {
	if s.Logf != nil {
		s.Logf(format, values...)
	}
}

func authorized(req *http.Request, token string) bool {
	if token == "" {
		return false // 生产接口不允许用“空 token”表达匿名访问。
	}
	want := "Bearer " + token
	got := req.Header.Get("Authorization")
	return len(got) == len(want) && subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("请求部分超过 %d 字节上限", limit)
	}
	return raw, nil
}
