package catalog

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
)

func TestOfflineBundleIsDeterministicAndContainsLockedProofs(t *testing.T) {
	root := filepath.Join(t.TempDir(), "repository")
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	trustDocument := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "example", KeyID: "testing", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	trust, err := pluginservice.NewTrustStore(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	local, err := pluginservice.NewRepository(root)
	if err != nil {
		t.Fatal(err)
	}
	repository := &pluginservice.SignedRepository{Local: local, Trust: trust}
	artifact, _ := publishTestArtifact(t, repository, privateKey, "1.0.0-dev.20260721.1.abcdef0")
	store, err := Open(root, repository)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := store.Resolve(pluginv1.ArtifactResolveRequest{
		Roots:  []pluginv1.ArtifactRequirement{{PluginID: artifact.PluginID, Constraint: "=" + artifact.Version}},
		Target: "backend", KernelVersion: "0.1.0", AllowedChannels: []string{"testing"},
		AllowedPublishers: []string{"example"}, AllowedPluginPrefixes: []string{"com.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	trustRaw, err := json.Marshal(trustDocument)
	if err != nil {
		t.Fatal(err)
	}
	bundleDirectory := filepath.Join(t.TempDir(), "bundles")
	first, err := CreateOfflineBundle(lock, trustRaw, repository, bundleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first.Path)
	second, err := CreateOfflineBundle(lock, trustRaw, repository, bundleDirectory)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second.Path)
	if first.SHA256 != second.SHA256 || first.Size != second.Size {
		t.Fatalf("same lock and trust snapshot must yield identical bundle: first=%+v second=%+v", first, second)
	}
	contents := readOfflineBundle(t, first.Path)
	for _, name := range []string{
		"vastplan.lock.json", "trust.json", "bundle.manifest.json",
		"artifacts/" + artifact.SHA256 + "/package.tar.gz",
		"artifacts/" + artifact.SHA256 + "/attestation.json",
	} {
		if _, ok := contents[name]; !ok {
			t.Fatalf("offline bundle missing %s: %v", name, mapKeys(contents))
		}
	}
	var embeddedLock pluginv1.ArtifactLock
	if err := json.Unmarshal(contents["vastplan.lock.json"], &embeddedLock); err != nil || embeddedLock.Digest != lock.Digest {
		t.Fatalf("embedded lock changed: lock=%#v err=%v", embeddedLock, err)
	}

	destinationLocal, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "imported"))
	if err != nil {
		t.Fatal(err)
	}
	destination := &pluginservice.SignedRepository{Local: destinationLocal, Trust: trust}
	importedLock, err := ImportOfflineBundle(first.Path, pluginservice.HTTPRepositoryAdapter{Repository: destination})
	if err != nil {
		t.Fatal(err)
	}
	if importedLock.Digest != lock.Digest {
		t.Fatalf("import changed lock: got=%s want=%s", importedLock.Digest, lock.Digest)
	}
	if imported, _, _, err := destination.ReadWithAttestation(pluginv1.ArtifactRef{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel}); err != nil || imported.SHA256 != artifact.SHA256 {
		t.Fatalf("imported artifact did not pass destination trust boundary: artifact=%#v err=%v", imported, err)
	}

	httpDestinationLocal, err := pluginservice.NewRepository(filepath.Join(t.TempDir(), "http-imported"))
	if err != nil {
		t.Fatal(err)
	}
	httpDestination := &pluginservice.SignedRepository{Local: httpDestinationLocal, Trust: trust}
	handler := &HTTPHandler{
		Store: store, ReadToken: "reader", BundleToken: "bundle", ImportToken: "publisher",
		BundleSource: repository, BundleDestination: pluginservice.HTTPRepositoryAdapter{Repository: httpDestination},
		TrustSnapshot: trustRaw, BundleDirectory: bundleDirectory, RequireTLS: true,
	}
	lockRequest, err := json.Marshal(lock)
	if err != nil {
		t.Fatal(err)
	}
	if status := bundleHTTP(t, handler, "/v1/catalog/bundles", "reader", lockRequest).Code; status != http.StatusUnauthorized {
		t.Fatalf("read token must not authorize bundle export: %d", status)
	}
	exported := bundleHTTP(t, handler, "/v1/catalog/bundles", "bundle", lockRequest)
	if exported.Code != http.StatusOK || exported.Header().Get("X-VastPlan-Bundle-SHA256") == "" {
		t.Fatalf("bundle export failed: status=%d body=%s", exported.Code, exported.Body.String())
	}
	if status := bundleHTTP(t, handler, "/v1/catalog/bundles/import", "bundle", exported.Body.Bytes()).Code; status != http.StatusUnauthorized {
		t.Fatalf("bundle export token must not authorize import: %d", status)
	}
	imported := bundleHTTP(t, handler, "/v1/catalog/bundles/import", "publisher", exported.Body.Bytes())
	if imported.Code != http.StatusOK {
		t.Fatalf("bundle HTTP import failed: status=%d body=%s", imported.Code, imported.Body.String())
	}
}

func bundleHTTP(t *testing.T, handler http.Handler, path, token string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "https://repository.test"+path, bytes.NewReader(body))
	request.TLS = &tls.ConnectionState{HandshakeComplete: true}
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func readOfflineBundle(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	contents := map[string][]byte{}
	for {
		header, err := reader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		raw, err := io.ReadAll(reader)
		if err != nil {
			t.Fatal(err)
		}
		contents[header.Name] = raw
	}
	return contents
}

func mapKeys(values map[string][]byte) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}
