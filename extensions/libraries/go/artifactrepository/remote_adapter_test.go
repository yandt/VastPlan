package artifactrepository

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestRemoteAdapterProjectsCatalogIntoProtocolSnapshot(t *testing.T) {
	profile, err := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "enterprise", Protocol: artifactrepositoryv1.ProtocolRemote,
		Endpoint: "https://repo.example", Channels: []string{"stable", "testing"},
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/catalog/artifacts" || request.URL.Query().Get("pageSize") != "100" {
			return nil, fmt.Errorf("unexpected request %s", request.URL.String())
		}
		body := `{"revision":9,"total":3,"page":1,"pageSize":100,"items":[` +
			`{"ref":{"pluginId":"cn.example.one","version":"1.0.0","channel":"stable"},"sha256":"` + strings.Repeat("a", 64) + `","repositoryRevision":7,"lifecycleStatus":"active","name":"ignored"},` +
			`{"ref":{"pluginId":"cn.example.two","version":"2.0.0-dev.1","channel":"testing"},"sha256":"` + strings.Repeat("b", 64) + `","repositoryRevision":8,"lifecycleStatus":"deprecated"},` +
			`{"ref":{"pluginId":"cn.example.revoked","version":"1.0.0","channel":"stable"},"sha256":"` + strings.Repeat("c", 64) + `","repositoryRevision":9,"lifecycleStatus":"revoked"}]}`
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(body))}, nil
	})}
	repository := &RemoteRepository{BaseURL: profile.Endpoint, Token: "reader", Trust: &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}, Client: client}
	adapter, err := NewRemoteAdapter(profile, repository)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := adapter.CatalogSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Revision != 9 || len(snapshot.Items) != 2 || snapshot.Items[0].Protocol != artifactrepositoryv1.ProtocolRemote || snapshot.Items[0].ProfileDigest != profile.Digest() {
		t.Fatalf("remote Catalog 投影无效: %+v", snapshot)
	}
}

func TestRemoteAdapterResolvesAndVerifiesArtifactLock(t *testing.T) {
	profile, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "enterprise", Protocol: artifactrepositoryv1.ProtocolRemote,
		Endpoint: "https://repo.example", Channels: []string{"stable", "testing"},
	})
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.plugin", Version: "1.0.0", Channel: "stable"}
	lock := pluginv1.ArtifactLock{
		SchemaVersion: "v1", RepositoryRevision: 3, Target: "backend", KernelVersion: "0.1.0",
		Roots:    []pluginv1.ArtifactRequirement{{PluginID: ref.PluginID, Constraint: "=1.0.0", Channel: "stable"}},
		Packages: []pluginv1.ArtifactLockPackage{{Ref: ref, SHA256: strings.Repeat("a", 64), Size: 10, Publisher: "example", KeyID: "release", RepositoryRevision: 3}},
	}
	lock.Digest, _ = pluginv1.ArtifactLockDigest(lock)
	responseRaw, _ := json.Marshal(lock)
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method != http.MethodPost || request.URL.Path != "/v1/catalog/resolve" || request.Header.Get("Authorization") != "Bearer reader" {
			return nil, fmt.Errorf("unexpected resolve request")
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(string(responseRaw)))}, nil
	})}
	adapter, err := NewRemoteAdapter(profile, &RemoteRepository{BaseURL: profile.Endpoint, Token: "reader", Trust: &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := adapter.ResolveLock(context.Background(), pluginv1.ArtifactResolveRequest{
		Roots:  []pluginv1.ArtifactRequirement{{PluginID: ref.PluginID, Constraint: "=1.0.0", Channel: "stable"}},
		Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"stable"}, AllowedPublishers: []string{"example"},
	})
	if err != nil || resolved.Digest != lock.Digest {
		t.Fatalf("remote Resolver 锁无效: lock=%+v err=%v", resolved, err)
	}
}

func TestRemoteAdapterRejectsProfileEndpointDrift(t *testing.T) {
	profile, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "enterprise", Protocol: artifactrepositoryv1.ProtocolRemote,
		Endpoint: "https://repo.example", Channels: []string{"testing"},
	})
	repository := &RemoteRepository{BaseURL: "https://other.example", Trust: &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}}
	if _, err := NewRemoteAdapter(profile, repository); err == nil {
		t.Fatal("remote Adapter 不得脱离 Profile endpoint")
	}
}

func TestRemoteAdapterReadsContentAddressedAssessmentReport(t *testing.T) {
	profile, _ := artifactrepositoryv1.ValidateProfile(artifactrepositoryv1.Profile{
		Version: 1, ID: "enterprise", Protocol: artifactrepositoryv1.ProtocolRemote,
		Endpoint: "https://repo.example", Channels: []string{"stable", "testing"},
	})
	report := []byte(`{"Results":[]}`)
	sum := sha256.Sum256(report)
	digest := hex.EncodeToString(sum[:])
	client := &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/assessment-reports/"+digest || request.Header.Get("Authorization") != "Bearer reader" {
			return nil, fmt.Errorf("unexpected report request")
		}
		return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Body: io.NopCloser(strings.NewReader(string(report)))}, nil
	})}
	adapter, err := NewRemoteAdapter(profile, &RemoteRepository{BaseURL: profile.Endpoint, Token: "reader", Trust: &TrustStore{keys: map[string]ed25519.PublicKey{}, meta: map[string]TrustKey{}}, Client: client})
	if err != nil {
		t.Fatal(err)
	}
	actual, err := adapter.ReadAssessmentReport(context.Background(), digest)
	if err != nil || string(actual) != string(report) {
		t.Fatalf("远端报告读取失败: raw=%s err=%v", actual, err)
	}
}
