package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
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
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	mux.HandleFunc("/v1/transit/", devTransit)
	r.vault = &http.Server{Addr: r.options.vaultListen, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	listener, err := net.Listen("tcp", r.options.vaultListen)
	if err != nil {
		return fmt.Errorf("监听开发 Vault Transit: %w", err)
	}
	go func() { _ = r.vault.Serve(listener) }()
	return nil
}

func devTransit(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Header.Get("X-Vault-Token") != "vastplan-local-vault-token" {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	operation := strings.TrimPrefix(request.URL.Path, "/v1/transit/")
	if !strings.HasPrefix(operation, "encrypt/") && !strings.HasPrefix(operation, "rewrap/") {
		http.NotFound(w, request)
		return
	}
	var payload map[string]string
	decoder := json.NewDecoder(http.MaxBytesReader(w, request.Body, 1<<20))
	if err := decoder.Decode(&payload); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	source := payload["plaintext"]
	if source == "" {
		source = payload["ciphertext"]
	}
	if source == "" {
		http.Error(w, "missing transit input", http.StatusBadRequest)
		return
	}
	digest := sha256.Sum256([]byte(source))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]string{"ciphertext": "vault:v1:" + base64.RawURLEncoding.EncodeToString(digest[:])}})
}

func (r *runtime) startProxy() error {
	target, _ := url.Parse("https://" + r.options.portalListen)
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Transport = &http.Transport{TLSClientConfig: insecureLocalTLS()}
	original := proxy.Director
	proxy.Director = func(request *http.Request) {
		original(request)
		if _, err := request.Cookie("vastplan_session"); err != nil {
			request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: devAdminToken})
		}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__vastplan_dev/status", r.status)
	if r.hmr != nil {
		mux.HandleFunc("/__vastplan_dev/events", r.hmr.events)
		mux.HandleFunc("/__vastplan_dev/runtime", r.hmr.runtime)
		mux.HandleFunc("/__vastplan_dev/modules/", r.hmr.module)
		mux.HandleFunc("/assets/", r.hmr.portalAssets)
		mux.HandleFunc("/", func(w http.ResponseWriter, request *http.Request) {
			if isPortalKernelRoute(request.URL.Path) {
				proxy.ServeHTTP(w, request)
				return
			}
			r.hmr.portalAssets(w, request)
		})
	} else {
		mux.Handle("/", proxy)
	}
	r.proxy = &http.Server{Addr: r.options.listen, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	listener, err := net.Listen("tcp", r.options.listen)
	if err != nil {
		return fmt.Errorf("监听开发网关: %w", err)
	}
	go func() { _ = r.proxy.Serve(listener) }()
	return nil
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
	r.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	status := map[string]any{
		"ready": ready, "portal": "http://" + r.options.listen + "/operations", "runDir": r.runDir,
		"mode": "local-development", "productionEquivalent": false,
		"hot": r.options.hot, "startupPublication": r.options.applyPlatform,
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
	if r.hmr != nil {
		generation, lastError := r.hmr.status()
		status["hotGeneration"], status["hotError"] = generation, lastError
	}
	_ = json.NewEncoder(w).Encode(status)
}
