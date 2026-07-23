// Package artifactapi 提供制品仓库的最小 HTTPS HTTP 传输层。
//
// 本包不解释发布者信任、签名或制品内容；这些 fail-closed 决策由 Repository
// 适配器完成。这样仓库作为基础插件运行时，HTTP 暴露面不会重新落回内核。
package artifactapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

const (
	DefaultMaxArtifactBytes   = int64(256 << 20)
	maxAttestationBytes       = int64(2 << 20)
	maxProvenanceBytes        = int64(2 << 20)
	maxVerificationBytes      = int64(256 << 10)
	maxSecurityAdmissionBytes = int64(256 << 10)
)

// Repository 是 HTTP 层所需的最小仓库适配器。实现必须在 Publish 和 Read 内
// 完成签名、信任和内容校验；Server 不把来自网络的输入视为可信。
type Repository interface {
	Publish(attestationRaw, packageBytes []byte) (pluginv1.Artifact, error)
	Read(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, []byte, error)
}

type ProvenanceRepository interface {
	PublishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw []byte) (pluginv1.Artifact, error)
	ReadWithProvenance(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, []byte, []byte, []byte, error)
}

type SupplyChainRepository interface {
	PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, securityAdmissionRaw []byte) (pluginv1.Artifact, error)
	ReadWithSupplyChain(ref pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, []byte, []byte, []byte, []byte, error)
}

// Server 暴露最小远端制品 API。读写令牌分离；RequireTLS 默认应为 true，
// 只有进程内测试或明确的本地开发反向代理场景才关闭。
type Server struct {
	Repository   Repository
	ReadToken    string
	PublishToken string
	RequireTLS   bool
	MaxBytes     int64
	Logf         func(string, ...any)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	if s.Repository == nil {
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

func (s *Server) publish(w http.ResponseWriter, req *http.Request) {
	if !authorized(req, s.PublishToken) {
		s.log("artifact.publish_denied remote=%s", req.RemoteAddr)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	maxBytes := s.maxBytes()
	req.Body = http.MaxBytesReader(w, req.Body, maxBytes+maxAttestationBytes+maxProvenanceBytes+maxVerificationBytes+maxSecurityAdmissionBytes+(1<<20))
	reader, err := req.MultipartReader()
	if err != nil {
		http.Error(w, "multipart required", http.StatusBadRequest)
		return
	}
	var attestationRaw, packageBytes, provenanceRaw, verificationRaw, securityAdmissionRaw []byte
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
		case "provenance":
			provenanceRaw, err = readLimited(part, maxProvenanceBytes)
		case "provenance-verification":
			verificationRaw, err = readLimited(part, maxVerificationBytes)
		case "security-admission":
			securityAdmissionRaw, err = readLimited(part, maxSecurityAdmissionBytes)
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
	var artifact pluginv1.Artifact
	if repository, ok := s.Repository.(SupplyChainRepository); ok {
		artifact, err = repository.PublishWithSupplyChain(attestationRaw, packageBytes, provenanceRaw, verificationRaw, securityAdmissionRaw)
	} else if len(securityAdmissionRaw) != 0 {
		err = errors.New("repository does not support security admission")
	} else if repository, ok := s.Repository.(ProvenanceRepository); ok {
		artifact, err = repository.PublishWithProvenance(attestationRaw, packageBytes, provenanceRaw, verificationRaw)
	} else if len(provenanceRaw) != 0 || len(verificationRaw) != 0 {
		err = errors.New("repository does not support provenance")
	} else {
		artifact, err = s.Repository.Publish(attestationRaw, packageBytes)
	}
	if err != nil {
		s.log("artifact.publish_rejected error=%v", err)
		http.Error(w, "artifact rejected", http.StatusUnprocessableEntity)
		return
	}
	s.log("artifact.published plugin=%s version=%s channel=%s sha256=%s", artifact.PluginID, artifact.Version, artifact.Channel, artifact.SHA256)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(artifact)
}

func (s *Server) read(w http.ResponseWriter, req *http.Request) {
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
	ref := pluginv1.ArtifactRef{PluginID: pluginID, Version: version, Channel: channel}
	var artifact pluginv1.Artifact
	var packageBytes, attestation, provenanceRaw, verificationRaw, securityAdmissionRaw []byte
	var err error
	if repository, ok := s.Repository.(SupplyChainRepository); ok {
		artifact, packageBytes, attestation, provenanceRaw, verificationRaw, securityAdmissionRaw, err = repository.ReadWithSupplyChain(ref)
	} else if repository, ok := s.Repository.(ProvenanceRepository); ok {
		artifact, packageBytes, attestation, provenanceRaw, verificationRaw, err = repository.ReadWithProvenance(ref)
	} else {
		artifact, packageBytes, attestation, err = s.Repository.Read(ref)
	}
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
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(attestation)
	case "provenance":
		if len(provenanceRaw) == 0 {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.dsse.envelope.v1+json")
		_, _ = w.Write(provenanceRaw)
	case "provenance-verification":
		if len(verificationRaw) == 0 {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(verificationRaw)
	case "security-admission":
		if len(securityAdmissionRaw) == 0 {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(securityAdmissionRaw)
	default:
		http.NotFound(w, req)
		return
	}
	s.log("artifact.read plugin=%s version=%s channel=%s sha256=%s", artifact.PluginID, artifact.Version, artifact.Channel, artifact.SHA256)
}

func (s *Server) maxBytes() int64 {
	if s.MaxBytes > 0 {
		return s.MaxBytes
	}
	return DefaultMaxArtifactBytes
}

func (s *Server) log(format string, values ...any) {
	if s.Logf != nil {
		s.Logf(format, values...)
	}
}

func authorized(req *http.Request, token string) bool {
	if token == "" {
		return false
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
		return nil, fmt.Errorf("request part exceeds %d byte limit", limit)
	}
	return raw, nil
}
