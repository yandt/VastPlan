package pluginservice

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

func TestRemoteRepository_TLSAuthPublishAndRead(t *testing.T) {
	packageBytes, artifact := testArtifact(t)
	publicKey, privateKey, _ := ed25519.GenerateKey(nil)
	trust, _ := NewTrustStore(TrustDocumentForPublicKeys(TrustKey{
		Publisher: "example", KeyID: "release", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}))
	local, _ := NewRepository(filepath.Join(t.TempDir(), "repository"))
	handler := &artifactapi.Server{
		Repository: HTTPRepositoryAdapter{Repository: &SignedRepository{Local: local, Trust: trust}},
		ReadToken:  "reader-secret", PublishToken: "publisher-secret", RequireTLS: true,
	}
	client := &http.Client{Transport: handlerTransport{handler: handler}}
	attestation, _ := SignArtifact(artifact, "example", "release", privateKey, time.Now().UTC())

	publisher := &RemoteRepository{
		BaseURL: "https://artifacts.example.test", Token: "publisher-secret", Trust: trust, Client: client,
	}
	if _, err := publisher.PublishRemote(context.Background(), attestation, packageBytes); err != nil {
		t.Fatalf("HTTPS 发布失败: %v", err)
	}
	reader := &RemoteRepository{
		BaseURL: "https://artifacts.example.test", Token: "reader-secret", Trust: trust, Client: client,
	}
	ref := Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}
	got, downloaded, err := reader.Read(ref)
	if err != nil {
		t.Fatalf("HTTPS 读取失败: %v", err)
	}
	if !sameArtifact(got, artifact) || string(downloaded) != string(packageBytes) {
		t.Fatal("远端读取必须返回签名绑定的原始制品")
	}

	reader.Token = "wrong"
	if _, _, err := reader.Read(ref); err == nil {
		t.Fatal("错误读令牌必须被拒绝")
	}
}

type handlerTransport struct{ handler http.Handler }

func (t handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.TLS = &tls.ConnectionState{HandshakeComplete: true}
	clone.RemoteAddr = "127.0.0.1:12345"
	recorder := httptest.NewRecorder()
	t.handler.ServeHTTP(recorder, clone)
	return recorder.Result(), nil
}

func TestRemoteRepository_RejectsPlainHTTPByDefault(t *testing.T) {
	trust := &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}
	repository := &RemoteRepository{BaseURL: "http://artifacts.example.test", Trust: trust}
	if _, _, _, err := repository.validate(); err == nil {
		t.Fatal("生产远端仓库必须拒绝明文 HTTP")
	}
	repository.AllowHTTP = true
	if _, _, _, err := repository.validate(); err != nil {
		t.Fatalf("显式本地开发模式应允许 HTTP: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRemoteRepositoryOnlyTreatsMissingAttestationAsSourceNotFound(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound, Status: "404 Not Found",
			Body: io.NopCloser(strings.NewReader("missing")),
		}, nil
	})}
	repository := &RemoteRepository{}
	if _, err := repository.get(context.Background(), client, "https://example/attestation", 1024, true); !errors.Is(err, artifacttrust.ErrNotFound) {
		t.Fatalf("证明不存在应允许尝试下一来源: %v", err)
	}
	if _, err := repository.get(context.Background(), client, "https://example/package", 1024, false); err == nil || errors.Is(err, artifacttrust.ErrNotFound) {
		t.Fatalf("证明存在后包体缺失是仓库损坏，不得静默换源: %v", err)
	}
}
