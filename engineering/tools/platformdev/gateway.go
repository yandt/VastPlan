package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"
)

func (r *runtime) startNATS() error {
	host, port, err := splitAddress(r.options.natsListen)
	if err != nil {
		return err
	}
	if port == 0 {
		port = -1
	}
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(r.persistentStateRoot(), "nats"), Host: host, Port: port,
		NoLog: true, NoSigs: true,
	})
	if err != nil {
		return fmt.Errorf("创建嵌入式 NATS: %w", err)
	}
	go server.Start()
	if !server.ReadyForConnections(10 * time.Second) {
		return errors.New("嵌入式 NATS 未就绪")
	}
	address, ok := server.Addr().(*net.TCPAddr)
	if !ok {
		server.Shutdown()
		return errors.New("嵌入式 NATS 未监听 TCP")
	}
	r.options.natsListen = net.JoinHostPort(host, fmt.Sprintf("%d", address.Port))
	r.nats = server
	return nil
}

func (r *runtime) startVault() error {
	keyDirectory := filepath.Join(r.persistentStateRoot(), "vault")
	if err := ensurePrivateDirectory(keyDirectory); err != nil {
		return fmt.Errorf("准备开发 Vault Transit 目录: %w", err)
	}
	keyPath := filepath.Join(keyDirectory, "transit.key")
	if err := ensurePrivate32ByteKey(keyPath, "开发 Vault Transit key"); err != nil {
		return err
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		return fmt.Errorf("读取开发 Vault Transit key: %w", err)
	}
	transit, err := newDevelopmentTransit(key, "vastplan-local")
	for index := range key {
		key[index] = 0
	}
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.Handle("/v1/transit/", transit)
	r.vault = &http.Server{Addr: r.options.vaultListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", r.options.vaultListen)
	if err != nil {
		return fmt.Errorf("监听开发 Vault Transit: %w", err)
	}
	go func() { _ = r.vault.Serve(listener) }()
	return nil
}

type developmentTransit struct {
	aead cipher.AEAD
	key  string
}

func newDevelopmentTransit(key []byte, name string) (*developmentTransit, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("初始化开发 Vault Transit: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("初始化开发 Vault Transit GCM: %w", err)
	}
	return &developmentTransit{aead: aead, key: name}, nil
}

func (t *developmentTransit) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Header.Get("X-Vault-Token") != "vastplan-local-vault-token" {
		writeTransitError(w, http.StatusForbidden, "forbidden")
		return
	}
	parts := strings.Split(strings.TrimPrefix(request.URL.Path, "/v1/transit/"), "/")
	if len(parts) != 2 || parts[1] != t.key || (parts[0] != "encrypt" && parts[0] != "decrypt" && parts[0] != "rewrap") {
		http.NotFound(w, request)
		return
	}
	var payload map[string]string
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		writeTransitError(w, http.StatusBadRequest, "invalid payload")
		return
	}
	var output map[string]string
	var err error
	switch parts[0] {
	case "encrypt":
		var plaintext []byte
		plaintext, err = base64.StdEncoding.DecodeString(payload["plaintext"])
		if err == nil && len(plaintext) > 0 {
			output, err = t.encrypt(plaintext)
		} else if err == nil {
			err = errors.New("missing plaintext")
		}
		zeroBytes(plaintext)
	case "decrypt":
		var plaintext []byte
		plaintext, err = t.decrypt(payload["ciphertext"])
		if err == nil {
			output = map[string]string{"plaintext": base64.StdEncoding.EncodeToString(plaintext)}
		}
		zeroBytes(plaintext)
	case "rewrap":
		var plaintext []byte
		plaintext, err = t.decrypt(payload["ciphertext"])
		if err == nil {
			output, err = t.encrypt(plaintext)
		}
		zeroBytes(plaintext)
	}
	if err != nil {
		writeTransitError(w, http.StatusBadRequest, "invalid transit input")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": output})
}

func (t *developmentTransit) encrypt(plaintext []byte) (map[string]string, error) {
	nonce := make([]byte, t.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	sealed := t.aead.Seal(nonce, nonce, plaintext, []byte(t.key))
	return map[string]string{"ciphertext": "vault:v1:" + base64.RawURLEncoding.EncodeToString(sealed)}, nil
}

func (t *developmentTransit) decrypt(value string) ([]byte, error) {
	encoded := strings.TrimPrefix(value, "vault:v1:")
	if encoded == value || encoded == "" {
		return nil, errors.New("invalid ciphertext")
	}
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(sealed) <= t.aead.NonceSize() {
		return nil, errors.New("invalid ciphertext")
	}
	nonce := sealed[:t.aead.NonceSize()]
	return t.aead.Open(nil, nonce, sealed[t.aead.NonceSize():], []byte(t.key))
}

func writeTransitError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []string{message}})
}

func zeroBytes(value []byte) {
	for index := range value {
		value[index] = 0
	}
}

func (r *runtime) startProxy() error {
	target, _ := url.Parse("http://" + r.options.portalListen)
	proxy := httputil.NewSingleHostReverseProxy(target)
	original := proxy.Director
	proxy.Director = func(request *http.Request) {
		source := request.Clone(request.Context())
		original(request)
		r.identity.decorateUpstream(source, request)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__vastplan_dev/status", r.status)
	if r.hmr != nil {
		mux.HandleFunc("/__vastplan_dev/events", r.hmr.events)
		mux.HandleFunc("/__vastplan_dev/runtime", r.hmr.runtime)
		mux.HandleFunc("/__vastplan_dev/modules/", r.hmr.module)
		mux.HandleFunc("/assets/", r.hmr.portalAssets)
		// Page requests must always cross the Portal Host identity boundary. HMR
		// owns immutable development assets only; it must never become a second,
		// unauthenticated page server.
		mux.Handle("/", developmentPortalProxy(proxy))
	} else {
		mux.Handle("/", developmentPortalProxy(proxy))
	}
	r.proxy = &http.Server{Addr: r.options.listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", r.options.listen)
	if err != nil {
		return fmt.Errorf("监听开发网关: %w", err)
	}
	go func() { _ = r.proxy.Serve(listener) }()
	return nil
}

// developmentPortalProxy gives the local Seed one canonical entry path. The
// access profile intentionally covers the whole loopback host, while its only
// governed Portal is mounted at /operations. Redirecting before authentication
// prevents a valid login from preserving returnTo=/ and then requesting a
// RuntimeSpec for a route that no Portal owns.
func developmentPortalProxy(upstream http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/" && (request.Method == http.MethodGet || request.Method == http.MethodHead) {
			w.Header().Set("Cache-Control", "no-store")
			http.Redirect(w, request, "/operations", http.StatusTemporaryRedirect)
			return
		}
		upstream.ServeHTTP(w, request)
	})
}

func isPortalKernelRoute(path string) bool {
	for _, prefix := range []string{"/v1", "/auth", "/api"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}
	return false
}

func (r *runtime) status(w http.ResponseWriter, _ *http.Request) {
	r.mu.RLock()
	ready := r.ready
	recovery := r.recovery
	platformPhase, platformError := r.platformPhase, r.platformError
	r.mu.RUnlock()
	platformControl := map[string]any{"phase": platformPhase, "profile": r.platformControlProfilePath()}
	if platformError != "" {
		platformControl["error"] = platformError
	}
	w.Header().Set("Content-Type", "application/json")
	status := map[string]any{
		"ready": ready, "portal": "http://" + r.options.listen + "/operations", "runDir": r.runDir,
		"mode": "local-development", "productionEquivalent": false,
		"identity": map[string]any{"protocol": r.identity.name(), "autoLogin": r.options.autoLogin},
		"hot":      r.options.hot, "startupPublication": r.options.applyPlatform,
		"platformControl": platformControl,
		"repositories": map[string]any{
			"seed": map[string]any{"url": "https://" + r.options.seedArtifactListen, "persistent": false},
			"testing": map[string]any{
				"protocol": r.repositoryProfile.Protocol, "endpoint": r.repositoryProfile.Endpoint,
				"profileDigest": r.repositoryProfile.Digest(), "persistent": true,
				"ready": r.testingRepositoryReady(),
			},
		},
	}
	status["recoveryCapsule"] = recovery
	status["backendPluginDevelopment"] = backendPluginDevelopmentStatus(r.options.stateRoot)
	status["pluginLibrarySource"] = pluginLibrarySourceStatus(r.options.stateRoot)
	if r.hmr != nil {
		generation, lastError := r.hmr.status()
		status["hotGeneration"], status["hotError"] = generation, lastError
	}
	_ = json.NewEncoder(w).Encode(status)
}
