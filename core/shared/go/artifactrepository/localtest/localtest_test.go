package localtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

const testToken = "0123456789abcdef0123456789abcdef"

type memoryRepository struct {
	mu       sync.Mutex
	profile  artifactrepositoryv1.Profile
	envelope artifacttrust.Envelope
	receipt  artifactrepositoryv1.Receipt
}

func (r *memoryRepository) ReadExact(_ context.Context, ref pluginv1.ArtifactRef) (artifacttrust.Envelope, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !sameRef(exactRef(r.envelope), ref) {
		return artifacttrust.Envelope{}, artifacttrust.ErrNotFound
	}
	return r.envelope, nil
}

func (r *memoryRepository) Publish(_ context.Context, envelope artifacttrust.Envelope) (artifactrepositoryv1.Receipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.envelope = envelope
	r.receipt = artifactrepositoryv1.Receipt{
		SchemaVersion: 1, RepositoryID: r.profile.ID, Protocol: r.profile.Protocol,
		ProfileDigest: r.profile.Digest(), Ref: exactRef(envelope), SHA256: envelope.Artifact.SHA256, Revision: 1,
	}
	return r.receipt, nil
}

func (r *memoryRepository) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []artifactrepositoryv1.Receipt{}
	if r.receipt.Revision != 0 {
		items = append(items, r.receipt)
	}
	return artifactrepositoryv1.CatalogSnapshot{
		SchemaVersion: 1, RepositoryID: r.profile.ID, Protocol: r.profile.Protocol,
		ProfileDigest: r.profile.Digest(), Revision: r.receipt.Revision, Items: items,
	}, nil
}

func (*memoryRepository) ExpireWorkspace(context.Context) (artifactrepositoryv1.ExpireWorkspaceResult, error) {
	return artifactrepositoryv1.ExpireWorkspaceResult{SchemaVersion: 1, Revision: 2, Expired: 0}, nil
}

func TestUnixSocketClientServerRoundTrip(t *testing.T) {
	directory := shortTempDir(t)
	profile := testProfile(t, filepath.Join(directory, "repository.sock"), true)
	repository := &memoryRepository{profile: profile}
	server, err := NewServer(profile, repository, testToken)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := Listen(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = listener.Close()
		_ = os.Remove(filepath.Join(directory, "repository.sock"))
	}()
	httpServer := &http.Server{Handler: server, ReadHeaderTimeout: 2 * time.Second}
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()

	info, err := os.Stat(filepath.Join(directory, "repository.sock"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("socket 必须为 0600: mode=%v err=%v", info.Mode().Perm(), err)
	}
	client, err := NewClient(profile, testToken)
	if err != nil {
		t.Fatal(err)
	}
	envelope := testEnvelope(t)
	receipt, err := client.Publish(context.Background(), envelope)
	if err != nil || receipt.Ref != exactRef(envelope) {
		t.Fatalf("发布失败: receipt=%+v err=%v", receipt, err)
	}
	read, err := client.ReadExact(context.Background(), receipt.Ref)
	if err != nil || string(read.PackageBytes) != string(envelope.PackageBytes) {
		t.Fatalf("精确读取失败: err=%v", err)
	}
	snapshot, err := client.CatalogSnapshot(context.Background())
	if err != nil || snapshot.Revision != 1 || len(snapshot.Items) != 1 {
		t.Fatalf("Catalog 快照失败: snapshot=%+v err=%v", snapshot, err)
	}
	result, err := client.ExpireWorkspace(context.Background())
	if err != nil || result.Revision != 2 {
		t.Fatalf("workspace 过期失败: result=%+v err=%v", result, err)
	}
}

func TestServerRejectsMissingOrWrongProtocolBeforeRepository(t *testing.T) {
	profile := testProfile(t, "/tmp/vastplan-local-test.sock", false)
	server, err := NewServer(profile, &memoryRepository{profile: profile}, testToken)
	if err != nil {
		t.Fatal(err)
	}
	for _, protocol := range []string{"", artifactrepositoryv1.ProtocolRemote} {
		request := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		request.Header.Set(ProtocolHeader, protocol)
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		if response.Code != http.StatusPreconditionFailed {
			t.Fatalf("协议 %q 应拒绝，实际 %d", protocol, response.Code)
		}
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/catalog", nil)
	request.Header.Set(ProtocolHeader, artifactrepositoryv1.ProtocolLocalTest)
	request.Header.Set("Authorization", testToken)
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("缺少 Bearer scheme 必须拒绝，实际 %d", response.Code)
	}
}

func TestPrivateSocketAndTokenAreMandatory(t *testing.T) {
	directory := shortTempDir(t)
	if err := os.Chmod(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	profile := testProfile(t, filepath.Join(directory, "repository.sock"), false)
	if listener, err := Listen(profile); err == nil {
		_ = listener.Close()
		t.Fatal("宽权限目录不得承载 local-test socket")
	}
	if _, err := NewClient(profile, "short"); err == nil {
		t.Fatal("短 token 必须拒绝")
	}
}

func TestClientMapsOnlyNotFoundToTrustedSentinel(t *testing.T) {
	directory := shortTempDir(t)
	profile := testProfile(t, filepath.Join(directory, "repository.sock"), false)
	repository := &memoryRepository{profile: profile}
	server, _ := NewServer(profile, repository, testToken)
	listener, err := Listen(profile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close(); _ = os.Remove(filepath.Join(directory, "repository.sock")) }()
	httpServer := &http.Server{Handler: server}
	go func() { _ = httpServer.Serve(listener) }()
	defer httpServer.Close()
	client, _ := NewClient(profile, testToken)
	_, err = client.ReadExact(context.Background(), pluginv1.ArtifactRef{PluginID: "cn.example.missing", Version: "1.0.0", Channel: "testing"})
	if !errors.Is(err, artifacttrust.ErrNotFound) {
		t.Fatalf("404 必须保留 not-found sentinel: %v", err)
	}
}

func testProfile(t *testing.T, socket string, workspace bool) artifactrepositoryv1.Profile {
	t.Helper()
	channels := []string{"testing"}
	var policy *artifactrepositoryv1.WorkspacePolicy
	if workspace {
		channels = append(channels, "workspace")
		policy = &artifactrepositoryv1.WorkspacePolicy{TTLSeconds: 300, MaxArtifacts: 10}
	}
	profile, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "development-local", Protocol: artifactrepositoryv1.ProtocolLocalTest,
		Endpoint: "unix://" + socket, Channels: channels, DevelopmentOnly: true, Workspace: policy,
	})
	if err != nil {
		t.Fatal(err)
	}
	return profile
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "vplt-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func testEnvelope(t *testing.T) artifacttrust.Envelope {
	t.Helper()
	packageBytes := []byte("test-package")
	digest := sha256.Sum256(packageBytes)
	sha := hex.EncodeToString(digest[:])
	manifest := json.RawMessage(`{"id":"cn.example.local-test","name":"Local test","description":"Local test plugin","version":"1.0.0-dev.1","publisher":"example","engines":{"backend":"^0.1"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`)
	return artifacttrust.Envelope{
		Artifact: pluginv1.Artifact{
			SchemaVersion: "v1", PluginID: "cn.example.local-test", Version: "1.0.0-dev.1", Channel: "testing",
			SHA256: sha, Size: int64(len(packageBytes)), Object: sha + ".tar.gz", Manifest: manifest,
		},
		PackageBytes: packageBytes,
		Proof:        json.RawMessage(`{"schemaVersion":"v1","signature":"test"}`),
	}
}
