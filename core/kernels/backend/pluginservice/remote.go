package pluginservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
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
	envelope, err := r.Fetch(context.Background(), ref)
	if err != nil {
		return Artifact{}, nil, err
	}
	if err := r.Trust.VerifyProof(envelope); err != nil {
		return Artifact{}, nil, fmt.Errorf("远端制品证明不可信: %w", err)
	}
	if err := ValidateArtifact(envelope.Artifact, envelope.PackageBytes); err != nil {
		return Artifact{}, nil, fmt.Errorf("远端制品内容校验失败: %w", err)
	}
	return envelope.Artifact, envelope.PackageBytes, nil
}

// Fetch 只获取远端未信任 Envelope。证明与内容的最终判定由 Node Agent 内核验证器
// 完成，避免未来可插拔制品源拥有“自证可信”的能力。
func (r *RemoteRepository) Fetch(ctx context.Context, ref Ref) (artifacttrust.Envelope, error) {
	base, client, maxBytes, err := r.validate()
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	endpoint := artifactEndpoint(base, ref)
	attestationRaw, err := r.get(ctx, client, endpoint+"/attestation", maxAttestationBytes, true)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	var attestation Attestation
	if err := decodeJSONStrict(attestationRaw, &attestation); err != nil {
		return artifacttrust.Envelope{}, fmt.Errorf("解析远端制品证明: %w", err)
	}
	artifact := attestation.Artifact
	if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel {
		return artifacttrust.Envelope{}, errors.New("远端制品证明与请求引用不一致")
	}
	// 内置 HTTPS 来源在下载大包前先做证明预检，避免明显不可信元数据消耗带宽；
	// Node Agent 收到完整 Envelope 后仍会在独立内核强制点再次验证，不能依赖此处。
	if err := r.Trust.Verify(attestation); err != nil {
		return artifacttrust.Envelope{}, fmt.Errorf("远端制品证明预检失败: %w", err)
	}
	if artifact.Size > maxBytes {
		return artifacttrust.Envelope{}, fmt.Errorf("远端制品大小 %d 超过客户端上限 %d", artifact.Size, maxBytes)
	}
	packageBytes, err := r.get(ctx, client, endpoint+"/package", maxBytes, false)
	if err != nil {
		return artifacttrust.Envelope{}, err
	}
	return artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes, Proof: attestationRaw}, nil
}

func (r *RemoteRepository) SourceName() string { return "remote-https" }

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

func (r *RemoteRepository) get(ctx context.Context, client *http.Client, endpoint string, limit int64, allowNotFound bool) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
		if allowNotFound && resp.StatusCode == http.StatusNotFound {
			return nil, fmt.Errorf("%w: %s", artifacttrust.ErrNotFound, endpoint)
		}
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
