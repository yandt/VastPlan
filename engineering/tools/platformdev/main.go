// Command platformdev assembles and runs the complete local platform-management
// stack. It is development-only orchestration: production keeps external NATS,
// Vault Transit, signed artifacts, TLS identities, and systemd-managed agents.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	devAdminToken  = "vastplan-local-platform-admin"
	authorToken    = "vastplan-local-portal-author"
	approverToken  = "vastplan-local-portal-approver"
	publisherToken = "vastplan-local-portal-publisher"
)

type options struct {
	root, stateRoot                                               string
	listen, portalListen, artifactListen, vaultListen, natsListen string
	hot                                                           bool
}

type child struct {
	name string
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.RWMutex
	err  error
}

type runtime struct {
	options  options
	runDir   string
	nats     *natsserver.Server
	vault    *http.Server
	proxy    *http.Server
	children []*child
	mu       sync.RWMutex
	ready    bool
	hmr      *frontendHMR
}

type packageSpec struct {
	id       string
	backend  bool
	frontend bool
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	var opts options
	flag.StringVar(&opts.root, "root", "", "VastPlan repository root")
	flag.StringVar(&opts.stateRoot, "state-root", ".vastplan/dev-platform", "development runtime root")
	flag.StringVar(&opts.listen, "listen", "127.0.0.1:18080", "developer gateway address")
	flag.StringVar(&opts.portalListen, "portal-listen", "127.0.0.1:18444", "internal Portal Edge address")
	flag.StringVar(&opts.artifactListen, "artifact-listen", "127.0.0.1:18443", "internal artifact repository address")
	flag.StringVar(&opts.vaultListen, "vault-listen", "127.0.0.1:18200", "development Vault Transit stub address")
	flag.StringVar(&opts.natsListen, "nats-listen", "127.0.0.1:0", "development NATS address; port 0 chooses a free port")
	flag.BoolVar(&opts.hot, "hot", true, "enable transactional frontend plugin hot replacement")
	flag.Parse()
	if err := run(opts); err != nil {
		log.Fatalf("本地平台管理中心退出: %v", err)
	}
}

func run(opts options) error {
	root, err := filepath.Abs(opts.root)
	if err != nil || opts.root == "" {
		return errors.New("必须提供有效的 -root")
	}
	opts.root = filepath.Clean(root)
	if !filepath.IsAbs(opts.stateRoot) {
		opts.stateRoot = filepath.Join(opts.root, opts.stateRoot)
	}
	releasePID, err := ownPIDFile(opts.stateRoot)
	if err != nil {
		return err
	}
	defer releasePID()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	runDir := filepath.Join(opts.stateRoot, "runs", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return fmt.Errorf("创建运行目录: %w", err)
	}
	r := &runtime{options: opts, runDir: runDir}
	if err := r.prepare(ctx); err != nil {
		return err
	}
	if err := r.start(ctx); err != nil {
		_ = r.shutdown()
		return err
	}

	log.Printf("平台管理中心已就绪: http://%s/operations", opts.listen)
	log.Printf("本地会话由开发网关注入；不要把这些端口暴露到非本机网络")
	select {
	case <-ctx.Done():
		log.Printf("收到停止信号，正在关闭本地平台管理中心")
	case err := <-firstChildExit(r.children):
		if err != nil {
			log.Printf("子进程意外退出: %v", err)
		}
		stop()
	}
	return r.shutdown()
}

func ownPIDFile(stateRoot string) (func(), error) {
	if err := os.MkdirAll(stateRoot, 0o700); err != nil {
		return nil, fmt.Errorf("创建开发运行目录: %w", err)
	}
	path := filepath.Join(stateRoot, "platformdev.pid")
	pid := []byte(fmt.Sprintf("%d\n", os.Getpid()))
	if err := os.WriteFile(path, pid, 0o600); err != nil {
		return nil, fmt.Errorf("写入开发编排器 PID: %w", err)
	}
	return func() {
		current, err := os.ReadFile(path)
		if err == nil && bytes.Equal(bytes.TrimSpace(current), bytes.TrimSpace(pid)) {
			_ = os.Remove(path)
		}
	}, nil
}

func (r *runtime) prepare(ctx context.Context) error {
	for _, dir := range []string{"bin", "dynamic", "repository", "installed", "state", "secrets", "artifact-store", "nats", "go-cache"} {
		if err := os.MkdirAll(filepath.Join(r.runDir, dir), 0o700); err != nil {
			return err
		}
	}
	log.Printf("[1/5] 构建 Backend 内核与插件")
	buildEnv := map[string]string{
		"CGO_ENABLED": "1",
		"OUT_DIR":     filepath.Join(r.runDir, "bin"),
		"GOCACHE":     filepath.Join(r.runDir, "go-cache"),
	}
	if err := r.command(ctx, buildEnv, "./engineering/tools/build.sh"); err != nil {
		return err
	}
	log.Printf("[2/5] 构建按需加载的 Portal 与前端插件")
	portalBuildEnv := map[string]string{"PORTAL_OUT_DIR": filepath.Join(r.runDir, "portal-assets")}
	if r.options.hot {
		portalBuildEnv["PORTAL_DEV_HMR"] = "1"
	}
	if err := r.command(ctx, portalBuildEnv, "./engineering/tools/build-frontend.sh"); err != nil {
		return fmt.Errorf("构建 Portal 失败（若依赖尚未安装，请先运行 pnpm install）: %w", err)
	}
	log.Printf("[3/5] 共同构建 bootstrap-policy dynamic-go 制品")
	if err := r.command(ctx, map[string]string{
		"OUT_DIR": filepath.Join(r.runDir, "dynamic"),
		"GOCACHE": filepath.Join(r.runDir, "go-cache"),
	}, "./engineering/tools/build-dynamic-go.sh"); err != nil {
		return err
	}
	log.Printf("[4/5] 装配本地不可变插件仓库")
	if err := r.packageArtifacts(ctx); err != nil {
		return err
	}
	log.Printf("[5/5] 生成仅限本地开发的 TLS、session 与服务配置")
	return r.writeFixtures()
}

func (r *runtime) command(ctx context.Context, extra map[string]string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = r.options.root
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	cmd.Env = mergedEnv(extra)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("执行 %s: %w", name, err)
	}
	return nil
}

func (r *runtime) packageArtifacts(ctx context.Context) error {
	repository := filepath.Join(r.runDir, "repository")
	specs := []packageSpec{
		{id: "com.vastplan.foundation.security.portal-access-policy", backend: true},
		{id: "com.vastplan.foundation.security.interaction-access-policy", backend: true},
		{id: "com.vastplan.foundation.security.platform-admin-access-policy", backend: true},
		{id: "com.vastplan.platform.interaction.broker", backend: true},
		{id: "com.vastplan.foundation.frontend.design-system.arco", frontend: true},
		{id: "com.vastplan.foundation.frontend.composition.standard", frontend: true},
		{id: "com.vastplan.foundation.frontend.layout.standard", frontend: true},
		{id: "com.vastplan.foundation.frontend.layout.top-navigation", frontend: true},
		{id: "com.vastplan.platform.configuration.portal-composer", backend: true, frontend: true},
		{id: "com.vastplan.platform.configuration.global-settings", backend: true, frontend: true},
		{id: "com.vastplan.platform.security.credentials", backend: true, frontend: true},
		{id: "com.vastplan.platform.data.relational.connection-manager", backend: true, frontend: true},
		{id: "com.vastplan.platform.artifacts.repository", backend: true, frontend: true},
		{id: "com.vastplan.platform.infrastructure.deployment-manager", backend: true, frontend: true},
		{id: "com.vastplan.demo-permission", backend: true},
		{id: "com.vastplan.hello-world", backend: true},
	}
	for _, spec := range specs {
		args := []string{"run", "./engineering/tools/pluginpackage", "-source", filepath.Join("extensions", "plugins", spec.id), "-repository", repository}
		if spec.backend {
			args = append(args, "-backend-bin", filepath.Join(r.runDir, "bin", spec.id))
		}
		if spec.frontend {
			args = append(args, "-frontend-bundle", filepath.Join(r.options.root, "extensions", "plugins", spec.id, "frontend", "dist", "index.js"))
		}
		if err := r.command(ctx, map[string]string{"GOCACHE": filepath.Join(r.runDir, "go-cache")}, "go", args...); err != nil {
			return fmt.Errorf("打包 %s: %w", spec.id, err)
		}
	}
	dynamicPackage, err := os.ReadFile(filepath.Join(r.runDir, "dynamic", "com.vastplan.foundation.security.bootstrap-policy.tar.gz"))
	if err != nil {
		return err
	}
	repo, err := pluginservice.NewRepository(repository)
	if err != nil {
		return err
	}
	if _, err := repo.Publish("stable", dynamicPackage); err != nil {
		return fmt.Errorf("发布 bootstrap-policy dynamic-go 制品: %w", err)
	}
	return nil
}

func (r *runtime) writeFixtures() error {
	certFile, keyFile := filepath.Join(r.runDir, "secrets", "tls-cert.pem"), filepath.Join(r.runDir, "secrets", "tls-key.pem")
	if err := writeTLS(certFile, keyFile); err != nil {
		return err
	}
	if err := writeTrust(filepath.Join(r.runDir, "secrets", "artifact-trust.json")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "vault-token"), []byte("vastplan-local-vault-token\n"), 0o600); err != nil {
		return err
	}
	if err := writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json")); err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		return err
	}
	rendered := bytes.ReplaceAll(template, []byte("__VASTPLAN_DEV_ROOT__"), []byte(filepath.ToSlash(r.runDir)))
	if err := os.WriteFile(filepath.Join(r.runDir, "platform-management-profile.json"), rendered, 0o600); err != nil {
		return err
	}
	managedProfile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(r.options.root, "engineering", "deploy", "managed-services-profile.json"))
	if err != nil {
		return err
	}
	catalog := backendcompositionv1.BackendPlatformCatalog{
		Document: compositioncommonv1.Document{Version: 1, Revision: 1, ID: "local-backend-platform"},
		Profiles: []backendcompositionv1.PlatformProfile{managedProfile},
		Bindings: []backendcompositionv1.BackendPlatformBinding{{TenantID: "local", DeploymentName: "managed-services", PlatformProfile: compositioncommonv1.Ref{ID: managedProfile.ID, Revision: managedProfile.Revision, Digest: managedProfile.Digest()}}},
	}
	catalogRaw, err := json.MarshalIndent(catalog, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "backend-platform-catalog.json"), catalogRaw, 0o600); err != nil {
		return err
	}
	return nil
}

func (r *runtime) start(ctx context.Context) error {
	if err := r.startNATS(); err != nil {
		return err
	}
	if err := r.startVault(); err != nil {
		return err
	}
	env := r.serviceEnv()
	kernel := filepath.Join(r.runDir, "dynamic", "backend-kernel")
	natsURL := "nats://" + r.options.natsListen
	nodeArgs := []string{
		"reconcile", "-nats-url", natsURL, "-nats-allow-insecure", "-nats-bootstrap", "-nats-replicas", "1",
		"-deployment", "platform-management", "-tenant", "local", "-node-id", "local-platform-node",
		"-labels", "environment=local-platform", "-repository", filepath.Join(r.runDir, "repository"),
		"-runtime-root", filepath.Join(r.runDir, "installed", "backend"), "-actual-state", filepath.Join(r.runDir, "state", "actual-state.json"),
		"-lock", filepath.Join(r.runDir, "state", "node-agent.lock"), "-third-party-plugin-policy", "deny",
		"-publisher-plugin-policies", "vastplan=allow-trusted", "-plugin-placement-default", "process-only",
		"-plugin-placements", "com.vastplan.foundation.security.bootstrap-policy=require-dynamic-go",
		"-backend-platform-catalog", filepath.Join(r.runDir, "backend-platform-catalog.json"), "-allow-development-plugins",
	}
	if _, err := r.startChild("node-agent", env, kernel, nodeArgs...); err != nil {
		return err
	}
	time.Sleep(750 * time.Millisecond)
	managedNodeArgs := []string{
		"reconcile", "-nats-url", natsURL, "-nats-allow-insecure",
		"-deployment", "managed-services", "-tenant", "local", "-node-id", "local-managed-node",
		"-labels", "environment=local-managed", "-repository", filepath.Join(r.runDir, "repository"),
		"-runtime-root", filepath.Join(r.runDir, "installed", "managed-services"), "-actual-state", filepath.Join(r.runDir, "state", "managed-services-actual.json"),
		"-lock", filepath.Join(r.runDir, "state", "managed-services.lock"), "-third-party-plugin-policy", "deny",
		"-publisher-plugin-policies", "vastplan=allow-trusted", "-plugin-placement-default", "process-only",
	}
	if _, err := r.startChild("managed-node-agent", env, kernel, managedNodeArgs...); err != nil {
		return err
	}
	controllerArgs := []string{
		"controlplane", "-nats-url", natsURL, "-nats-allow-insecure", "-bootstrap", "-replicas", "1",
		"-platform-profile", filepath.Join(r.runDir, "platform-management-profile.json"),
		"-application-composition", filepath.Join(r.options.root, "engineering", "deploy", "platform-management-application.json"),
		"-deployment-revision", "1", "-repository", filepath.Join(r.runDir, "repository"), "-controller",
		"-backend-platform-catalog", filepath.Join(r.runDir, "backend-platform-catalog.json"),
	}
	if _, err := r.startChild("controller", env, kernel, controllerArgs...); err != nil {
		return err
	}
	if err := waitForUnits(ctx, filepath.Join(r.runDir, "state", "actual-state.json"), 5, 90*time.Second); err != nil {
		return fmt.Errorf("平台 Backend 未收敛: %w", err)
	}
	portalArgs := []string{
		"portal-edge", "-listen", r.options.portalListen,
		"-tls-cert", filepath.Join(r.runDir, "secrets", "tls-cert.pem"), "-tls-key", filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"-session-file", filepath.Join(r.runDir, "secrets", "portal-sessions.json"),
		"-repository", filepath.Join(r.runDir, "repository"), "-install-root", filepath.Join(r.runDir, "installed", "portal"), "-allow-unsigned-local",
		"-frontend-delivery-origin", filepath.Join(r.runDir, "frontend-delivery-origin"),
		"-frontend-delivery-cache", filepath.Join(r.runDir, "frontend-delivery-cache"),
		"-composer-version", "1.0.0", "-composer-state-file", filepath.Join(r.runDir, "state", "portal-composer.json"),
		"-portal-platform-catalog", filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json"),
		"-interaction-broker-version", "0.1.0", "-interaction-broker-state-file", filepath.Join(r.runDir, "state", "interaction-broker.json"),
		"-portal-assets", filepath.Join(r.runDir, "portal-assets"), "-nats-url", natsURL, "-nats-allow-insecure",
	}
	if _, err := r.startChild("portal-edge", env, kernel, portalArgs...); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "https://"+r.options.portalListen+"/v1/csrf", 45*time.Second, true); err != nil {
		return fmt.Errorf("Portal Edge 未就绪: %w", err)
	}
	if err := publishPortal("https://" + r.options.portalListen); err != nil {
		return fmt.Errorf("发布初始 Portal 组合: %w", err)
	}
	if err := publishManagedService("https://" + r.options.portalListen); err != nil {
		return fmt.Errorf("发布初始在线服务组合: %w", err)
	}
	if err := waitForUnits(ctx, filepath.Join(r.runDir, "state", "managed-services-actual.json"), 1, 60*time.Second); err != nil {
		return fmt.Errorf("在线服务组合未收敛: %w", err)
	}
	if r.options.hot {
		if err := r.startFrontendHMR(ctx); err != nil {
			return err
		}
	}
	if err := r.startProxy(); err != nil {
		return err
	}
	r.mu.Lock()
	r.ready = true
	r.mu.Unlock()
	return nil
}

func (r *runtime) serviceEnv() map[string]string {
	return map[string]string{
		"VASTPLAN_CREDENTIALS_STATE_FILE":          filepath.Join(r.runDir, "state", "credentials.json"),
		"VASTPLAN_VAULT_ADDR":                      "http://" + r.options.vaultListen,
		"VASTPLAN_VAULT_TRANSIT_KEY":               "vastplan-local",
		"VASTPLAN_VAULT_TOKEN_FILE":                filepath.Join(r.runDir, "secrets", "vault-token"),
		"VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE": filepath.Join(r.runDir, "state", "database-connections.json"),
		"VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE":   filepath.Join(r.runDir, "state", "deployment-manager.json"),
		"VASTPLAN_ARTIFACT_LISTEN_ADDR":            r.options.artifactListen,
		"VASTPLAN_ARTIFACT_REPOSITORY":             filepath.Join(r.runDir, "artifact-store"),
		"VASTPLAN_ARTIFACT_TRUST":                  filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		"VASTPLAN_ARTIFACT_TLS_CERT":               filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
		"VASTPLAN_ARTIFACT_TLS_KEY":                filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"VASTPLAN_ARTIFACT_READ_TOKEN":             "vastplan-local-artifact-reader",
		"VASTPLAN_ARTIFACT_PUBLISH_TOKEN":          "vastplan-local-artifact-publisher",
	}
}

func (r *runtime) startChild(name string, env map[string]string, executable string, args ...string) (*child, error) {
	cmd := exec.Command(executable, args...)
	cmd.Dir = r.options.root
	cmd.Env = mergedEnv(env)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动 %s: %w", name, err)
	}
	item := &child{name: name, cmd: cmd, done: make(chan struct{})}
	r.children = append(r.children, item)
	go func() {
		err := cmd.Wait()
		item.mu.Lock()
		item.err = err
		item.mu.Unlock()
		close(item.done)
	}()
	log.Printf("已启动 %s pid=%d", name, cmd.Process.Pid)
	return item, nil
}

func (r *runtime) startNATS() error {
	host, port, err := splitAddress(r.options.natsListen)
	if err != nil {
		return err
	}
	if port == 0 {
		port = -1
	}
	server, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true, StoreDir: filepath.Join(r.runDir, "nats"), Host: host, Port: port,
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
			if request.URL.Path == "/v1" || strings.HasPrefix(request.URL.Path, "/v1/") {
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

func (r *runtime) status(w http.ResponseWriter, _ *http.Request) {
	r.mu.RLock()
	ready := r.ready
	r.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	status := map[string]any{
		"ready": ready, "portal": "http://" + r.options.listen + "/operations", "runDir": r.runDir,
		"mode": "local-development", "productionEquivalent": false,
		"hot": r.options.hot,
	}
	if r.hmr != nil {
		generation, lastError := r.hmr.status()
		status["hotGeneration"], status["hotError"] = generation, lastError
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (r *runtime) shutdown() error {
	r.mu.Lock()
	r.ready = false
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	if r.proxy != nil {
		_ = r.proxy.Shutdown(ctx)
	}
	for i := len(r.children) - 1; i >= 0; i-- {
		if process := r.children[i].cmd.Process; process != nil {
			_ = process.Signal(os.Interrupt)
		}
	}
	deadline := time.After(8 * time.Second)
	for _, item := range r.children {
		select {
		case <-item.done:
		case <-deadline:
			for _, remaining := range r.children {
				if remaining.cmd.Process != nil {
					_ = remaining.cmd.Process.Kill()
				}
			}
			return nil
		}
	}
	if r.vault != nil {
		_ = r.vault.Shutdown(ctx)
	}
	if r.nats != nil {
		r.nats.Shutdown()
		r.nats.WaitForShutdown()
	}
	return nil
}

func firstChildExit(children []*child) <-chan error {
	result := make(chan error, 1)
	for _, item := range children {
		item := item
		go func() {
			<-item.done
			item.mu.RLock()
			err := item.err
			item.mu.RUnlock()
			result <- fmt.Errorf("%s: %w", item.name, err)
		}()
	}
	return result
}

func waitForUnits(ctx context.Context, filename string, count int, timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	var last string
	for {
		raw, err := os.ReadFile(filename)
		if err == nil {
			var state struct {
				Units map[string]struct {
					Phase     string `json:"phase"`
					Readiness string `json:"readiness"`
					LastError string `json:"last_error"`
				} `json:"units"`
			}
			if json.Unmarshal(raw, &state) == nil {
				active := 0
				messages := make([]string, 0, len(state.Units))
				for id, unit := range state.Units {
					messages = append(messages, id+"="+unit.Phase+"/"+unit.Readiness+" "+unit.LastError)
					if unit.Phase == "active" && (unit.Readiness == "ready" || unit.Readiness == "") {
						active++
					}
				}
				sort.Strings(messages)
				last = strings.Join(messages, "; ")
				if len(state.Units) == count && active == count {
					return nil
				}
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("等待 %d 个 active unit 超时: %s", count, last)
		case <-ticker.C:
		}
	}
}

func waitHTTP(ctx context.Context, endpoint string, timeout time.Duration, insecure bool) error {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = insecureLocalTLS()
	}
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second}
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		response, err := client.Get(endpoint)
		if err == nil {
			_ = response.Body.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return err
		case <-ticker.C:
		}
	}
}

func publishPortal(baseURL string) error {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}, Timeout: 10 * time.Second}
	spec := map[string]any{
		"version": 1, "revision": 1, "id": "operations", "target": map[string]string{"kernel": "frontend"},
		"route": "/operations", "audience": []string{"portal.read"}, "plugins": []any{}, "config": map[string]any{},
		"branding": map[string]any{"title": "VastPlan 平台管理中心"},
	}
	status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, "/v1/portal-drafts", spec, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("create status=%d body=%s: %w", status, raw, err)
	}
	var revision struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &revision); err != nil || revision.ID == 0 {
		return errors.New("Composer 未返回有效 revision")
	}
	steps := []struct{ token, operation string }{{authorToken, "submit"}, {approverToken, "approve"}, {publisherToken, "publish"}}
	for _, step := range steps {
		path := fmt.Sprintf("/v1/portal-drafts/%d/%s", revision.ID, step.operation)
		status, raw, err = portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("%s status=%d body=%s: %w", step.operation, status, raw, err)
		}
	}
	// Published Application/Profile/Binding revisions are eligible inputs only.
	// Select the catalog-seeded Profile + Binding and make the initial live fact
	// explicit through the same CAS-protected Activation API used in production.
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-governance", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("governance status=%d body=%s: %w", status, raw, err)
	}
	var governance portalapi.GovernanceSnapshot
	if err := json.Unmarshal(raw, &governance); err != nil {
		return fmt.Errorf("decode governance: %w", err)
	}
	var binding portalapi.BindingRevision
	for _, candidate := range governance.Bindings {
		if candidate.PortalID == "operations" && candidate.Status == portalapi.StatusPublished && candidate.ID > binding.ID {
			binding = candidate
		}
	}
	if binding.ID == 0 {
		return errors.New("未找到 operations 的已发布 Portal Binding")
	}
	var profile portalapi.PlatformProfileRevision
	for _, candidate := range governance.Profiles {
		if candidate.ID == binding.ProfileRevisionID && candidate.Status == portalapi.StatusPublished {
			profile = candidate
			break
		}
	}
	if profile.ID == 0 {
		return fmt.Errorf("Binding #%d 引用的 Profile #%d 不可用", binding.ID, binding.ProfileRevisionID)
	}
	var expectedCurrentID uint64
	for _, candidate := range governance.Activations {
		if candidate.PortalID == "operations" && candidate.Status == portalapi.ActivationCurrent {
			expectedCurrentID = candidate.ID
			break
		}
	}
	activationRequest := portalapi.ActivationRequest{
		PortalID: "operations", ApplicationRevisionID: revision.ID, ProfileRevisionID: profile.ID,
		BindingRevisionID: binding.ID, ExpectedCurrentID: expectedCurrentID, Reason: "platformdev startup activation",
	}
	status, raw, err = portalRequest(client, baseURL, publisherToken, http.MethodPost, "/v1/portal-governance/activations", activationRequest, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("activate status=%d body=%s: %w", status, raw, err)
	}
	var activation portalapi.PortalActivation
	if err := json.Unmarshal(raw, &activation); err != nil {
		return fmt.Errorf("decode activation: %w", err)
	}
	if activation.Status != portalapi.ActivationCurrent {
		return fmt.Errorf("initial Portal activation failed: %+v", activation)
	}
	status, raw, err = portalRequest(client, baseURL, devAdminToken, http.MethodGet, "/v1/portal-runtime?path=/operations", nil, false)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("runtime status=%d body=%s: %w", status, raw, err)
	}
	return nil
}

func publishManagedService(baseURL string) error {
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: insecureLocalTLS()}, Timeout: 10 * time.Second}
	composition := map[string]any{
		"version": 1, "revision": 1, "id": "managed-services", "target": map[string]string{"kernel": "backend"},
		"metadata": map[string]string{"name": "managed-services"},
		"units": []any{map[string]any{
			"serviceClass": "application.backend",
			"spec": map[string]any{
				"id": "hello-service", "kind": "service", "enabled": true, "service_role": "backend", "replicas": 1,
				"placement": map[string]any{"nodeSelector": map[string]string{"environment": "local-managed"}},
				"plugins":   []any{map[string]string{"id": "com.vastplan.hello-world", "version": "0.1.0", "channel": "stable"}},
			},
		}},
	}
	basePath := "/v1/portals/operations/platform/services/deployment/deployment/service-revisions"
	status, raw, err := portalRequest(client, baseURL, authorToken, http.MethodPost, basePath, map[string]any{"composition": composition}, true)
	if err != nil || status != http.StatusOK {
		return fmt.Errorf("create service status=%d body=%s: %w", status, raw, err)
	}
	var revision struct {
		ID uint64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &revision); err != nil || revision.ID == 0 {
		return errors.New("Deployment Manager 未返回有效服务 revision")
	}
	steps := []struct{ token, operation string }{{authorToken, "submit"}, {approverToken, "approve"}, {publisherToken, "publish"}}
	for _, step := range steps {
		path := fmt.Sprintf("%s/%d/%s", basePath, revision.ID, step.operation)
		status, raw, err = portalRequest(client, baseURL, step.token, http.MethodPost, path, map[string]any{}, true)
		if err != nil || status != http.StatusOK {
			return fmt.Errorf("service %s status=%d body=%s: %w", step.operation, status, raw, err)
		}
	}
	return nil
}

func portalRequest(client *http.Client, baseURL, session, method, path string, payload any, csrf bool) (int, []byte, error) {
	csrfToken := ""
	if csrf {
		request, _ := http.NewRequest(http.MethodGet, baseURL+"/v1/csrf", nil)
		request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
		response, err := client.Do(request)
		if err != nil {
			return 0, nil, err
		}
		if response.StatusCode != http.StatusOK {
			_ = response.Body.Close()
			return response.StatusCode, nil, errors.New("csrf rejected")
		}
		var result struct {
			Token string `json:"token"`
		}
		err = json.NewDecoder(response.Body).Decode(&result)
		_ = response.Body.Close()
		if err != nil || result.Token == "" {
			return 0, nil, errors.New("invalid csrf response")
		}
		csrfToken = result.Token
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewReader(raw)
	}
	request, err := http.NewRequest(method, baseURL+path, body)
	if err != nil {
		return 0, nil, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: "vastplan_session", Value: session})
	if csrfToken != "" {
		request.AddCookie(&http.Cookie{Name: "vastplan_csrf", Value: csrfToken})
		request.Header.Set("X-VastPlan-CSRF", csrfToken)
	}
	response, err := client.Do(request)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	return response.StatusCode, raw, err
}

func writeSessions(filename string) error {
	type record struct {
		TokenSHA256 string   `json:"tokenSHA256"`
		ID          string   `json:"id"`
		TenantID    string   `json:"tenantId"`
		Roles       []string `json:"roles"`
		ExpiresAt   string   `json:"expiresAt"`
	}
	sessions := []struct {
		token, id string
		roles     []string
	}{
		{devAdminToken, "local-admin", []string{"platform.admin", "portal.read", "portal.compose", "portal.approve", "portal.publish"}},
		{authorToken, "local-author", []string{"portal.read", "portal.compose", "platform.deployment.read", "platform.deployment.compose"}},
		{approverToken, "local-approver", []string{"portal.read", "portal.approve", "platform.deployment.read", "platform.deployment.approve"}},
		{publisherToken, "local-publisher", []string{"portal.read", "portal.publish", "platform.deployment.read", "platform.deployment.publish"}},
	}
	doc := struct {
		Sessions []record `json:"sessions"`
	}{}
	for _, session := range sessions {
		digest := sha256.Sum256([]byte(session.token))
		doc.Sessions = append(doc.Sessions, record{
			TokenSHA256: hex.EncodeToString(digest[:]), ID: session.id, TenantID: "local", Roles: session.roles,
			ExpiresAt: time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(raw, '\n'), 0o600)
}

func writeTrust(filename string) error {
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	document := pluginservice.TrustDocumentForPublicKeys(pluginservice.TrustKey{
		Publisher: "vastplan", KeyID: "local-development", PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	})
	raw, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, append(raw, '\n'), 0o600)
}

func writeTLS(certFile, keyFile string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}
	template := x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "vastplan-local-development"},
		DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		return err
	}
	return os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600)
}

func splitAddress(address string) (string, int, error) {
	host, rawPort, err := net.SplitHostPort(address)
	if err != nil {
		return "", 0, err
	}
	var port int
	if _, err := fmt.Sscanf(rawPort, "%d", &port); err != nil || port < 0 {
		return "", 0, fmt.Errorf("非法端口 %q", rawPort)
	}
	if host != "127.0.0.1" && host != "localhost" {
		return "", 0, errors.New("开发服务只允许监听 loopback")
	}
	return host, port, nil
}

func mergedEnv(extra map[string]string) []string {
	values := map[string]string{}
	for _, item := range os.Environ() {
		if index := strings.IndexByte(item, '='); index > 0 {
			values[item[:index]] = item[index+1:]
		}
	}
	for key, value := range extra {
		values[key] = value
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]string, 0, len(keys))
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func insecureLocalTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12} // #nosec G402 -- generated loopback-only development certificate.
}
