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
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/pluginservice"
	"cdsoft.com.cn/VastPlan/core/shared/go/bootstrapinventory"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

const (
	devAdminToken  = "vastplan-local-platform-admin"
	authorToken    = "vastplan-local-portal-author"
	approverToken  = "vastplan-local-portal-approver"
	publisherToken = "vastplan-local-portal-publisher"
)

type options struct {
	root, stateRoot                                                                   string
	listen, portalListen, artifactListen, seedArtifactListen, vaultListen, natsListen string
	hot                                                                               bool
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
	id, frontendEntry, frontendServerEntry string
	backend, frontend                      bool
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	var opts options
	flag.StringVar(&opts.root, "root", "", "VastPlan repository root")
	flag.StringVar(&opts.stateRoot, "state-root", ".vastplan/dev-platform", "development runtime root")
	flag.StringVar(&opts.listen, "listen", "127.0.0.1:18080", "developer gateway address")
	flag.StringVar(&opts.portalListen, "portal-listen", "127.0.0.1:18444", "internal Portal Edge address")
	flag.StringVar(&opts.artifactListen, "artifact-listen", "127.0.0.1:18443", "internal artifact repository address")
	flag.StringVar(&opts.seedArtifactListen, "seed-artifact-listen", "127.0.0.1:18442", "seed artifact repository address")
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
	for _, dir := range []string{"installed", "state", "secrets", "artifact-store", "nats"} {
		if err := os.MkdirAll(filepath.Join(r.runDir, dir), 0o700); err != nil {
			return err
		}
	}
	for _, dir := range []string{r.persistentStateRoot(), r.testingRepositoryRoot(), r.testingRepositoryVolumes(), r.testingRepositorySecrets()} {
		if err := ensurePrivateDirectory(dir); err != nil {
			return fmt.Errorf("准备持久化开发目录: %w", err)
		}
	}
	log.Printf("[1/6] 生成仅限本地开发的 TLS、session、Seed 仓库配置与签名身份")
	if err := r.writeFixtures(); err != nil {
		return err
	}
	if err := r.prepareCachedBuilds(ctx); err != nil {
		return err
	}
	if err := r.signPackageRepository(); err != nil {
		return err
	}
	return nil
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

func (r *runtime) packageArtifacts(ctx context.Context, repository, binDir, frontendModulesDir, dynamicDir string) error {
	specs, err := discoverPackageSpecs(r.options.root)
	if err != nil {
		return err
	}
	for _, spec := range specs {
		args := []string{"run", "./engineering/tools/pluginpackage", "-source", filepath.Join("extensions", "plugins", spec.id), "-repository", repository}
		if spec.backend {
			args = append(args, "-backend-bin", filepath.Join(binDir, spec.id))
		}
		if spec.frontend {
			graphRoot := filepath.Join(frontendModulesDir, spec.id)
			args = append(args, "-frontend-graph", filepath.Join(graphRoot, "frontend", "dist", "vastplan.browser-graph.json"), "-frontend-graph-root", graphRoot)
			if spec.frontendServerEntry != "" {
				args = append(args, "-frontend-server-graph", filepath.Join(graphRoot, "frontend", "dist", "vastplan.server-graph.json"))
			}
		}
		if err := r.command(ctx, map[string]string{"GOCACHE": filepath.Join(r.options.stateRoot, "go-cache")}, "go", args...); err != nil {
			return fmt.Errorf("打包 %s: %w", spec.id, err)
		}
	}
	dynamicPackage, err := os.ReadFile(filepath.Join(dynamicDir, "cn.vastplan.foundation.security.bootstrap-policy.tar.gz"))
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

// signPackageRepository upgrades the locally built development repository to a
// signed Seed repository after it has been materialized from the reproducible
// build cache. The signing key is generated per run and is never cached.
func (r *runtime) signPackageRepository() error {
	repository, err := pluginservice.NewRepository(filepath.Join(r.runDir, "repository"))
	if err != nil {
		return err
	}
	trust, err := pluginservice.LoadTrustStore(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json"))
	if err != nil {
		return err
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(filepath.Join(r.runDir, "secrets", "artifact-signing.pem"))
	if err != nil {
		return err
	}
	refs, err := packageRepositoryRefs(filepath.Join(r.runDir, "repository"))
	if err != nil {
		return err
	}
	signed := &pluginservice.SignedRepository{Local: repository, Trust: trust}
	for _, ref := range refs {
		artifact, packageBytes, err := repository.Read(ref)
		if err != nil {
			return err
		}
		manifest, err := pluginv1.ParseManifest(artifact.Manifest)
		if err != nil {
			return fmt.Errorf("解析 %s 的制品清单: %w", ref.PluginID, err)
		}
		attestation, err := pluginservice.SignArtifact(artifact, manifest.Publisher, "local-development", privateKey, time.Now().UTC())
		if err != nil {
			return err
		}
		if _, err := signed.Publish(attestation, packageBytes); err != nil {
			return err
		}
	}
	if err := r.writeBootstrapInventory(repository, refs); err != nil {
		return err
	}
	log.Printf("[6/6] 已签署 %d 个本地 Seed 制品", len(refs))
	return nil
}

func (r *runtime) writeBootstrapInventory(repository *pluginservice.Repository, refs []pluginservice.Ref) error {
	items := make([]bootstrapinventory.Item, 0, len(refs))
	lkgIDs := map[string]struct{}{
		"cn.vastplan.foundation.security.platform-admin-access-policy": {},
		"cn.vastplan.platform.artifacts.storage.file":                  {},
		"cn.vastplan.platform.artifacts.repository":                    {},
	}
	lkg := make([]bootstrapinventory.Item, 0, len(lkgIDs))
	for _, ref := range refs {
		artifact, err := repository.ReadMetadata(ref)
		if err != nil {
			return err
		}
		item := bootstrapinventory.Item{Ref: ref, SHA256: artifact.SHA256}
		items = append(items, item)
		if _, ok := lkgIDs[ref.PluginID]; ok {
			lkg = append(lkg, item)
		}
	}
	inventory, err := bootstrapinventory.Normalize(bootstrapinventory.Inventory{
		Version: bootstrapinventory.Version, Generation: uint64(time.Now().UTC().UnixNano()), RepositoryID: "local-seed",
		Seed: items, LastKnownGood: lkg,
	})
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(inventory, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.runDir, "seed-inventory.json"), append(raw, '\n'), 0o600)
}

func packageRepositoryRefs(root string) ([]pluginservice.Ref, error) {
	refs := make([]pluginservice.Ref, 0)
	err := filepath.WalkDir(filepath.Join(root, "artifacts"), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || entry.Name() != "artifact.json" {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := pluginv1.ValidateArtifactMetadata(raw); err != nil {
			return err
		}
		var artifact pluginservice.Artifact
		if err := json.Unmarshal(raw, &artifact); err != nil {
			return err
		}
		refs = append(refs, pluginservice.Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(refs, func(i, j int) bool {
		left, right := refs[i], refs[j]
		if left.PluginID != right.PluginID {
			return left.PluginID < right.PluginID
		}
		if left.Version != right.Version {
			return left.Version < right.Version
		}
		return left.Channel < right.Channel
	})
	if len(refs) == 0 {
		return nil, errors.New("Seed 制品仓库为空")
	}
	return refs, nil
}

// discoverPackageSpecs makes plugin manifests the sole development-time source
// for frontend and native-Go package inputs. A new UI plugin therefore needs
// no platformdev allow-list entry before its source can be hot-reloaded.
func discoverPackageSpecs(root string) ([]packageSpec, error) {
	pluginsRoot := filepath.Join(root, "extensions", "plugins")
	directories, err := os.ReadDir(pluginsRoot)
	if err != nil {
		return nil, fmt.Errorf("读取插件目录: %w", err)
	}
	specs := make([]packageSpec, 0, len(directories))
	for _, directory := range directories {
		if !directory.IsDir() {
			continue
		}
		pluginRoot := filepath.Join(pluginsRoot, directory.Name())
		raw, err := os.ReadFile(filepath.Join(pluginRoot, "vastplan.plugin.json"))
		if err != nil {
			return nil, fmt.Errorf("读取插件 %s 清单: %w", directory.Name(), err)
		}
		manifest, err := pluginv1.ParseManifest(raw)
		if err != nil {
			return nil, fmt.Errorf("解析插件 %s 清单: %w", directory.Name(), err)
		}
		if manifest.ID != directory.Name() {
			return nil, fmt.Errorf("插件目录 %s 与清单 id %s 不一致", directory.Name(), manifest.ID)
		}
		frontendEntry := strings.TrimSpace(manifest.Entry["frontend"])
		frontendServerEntry := strings.TrimSpace(manifest.Entry["frontendServer"])
		if frontendEntry != "" && (!strings.HasPrefix(frontendEntry, "frontend/dist/") || strings.Contains(frontendEntry, "..")) {
			return nil, fmt.Errorf("插件 %s entry.frontend 必须位于 frontend/dist/", manifest.ID)
		}
		if frontendServerEntry != "" && (!strings.HasPrefix(frontendServerEntry, "frontend/dist/") || strings.Contains(frontendServerEntry, "..")) {
			return nil, fmt.Errorf("插件 %s entry.frontendServer 必须位于 frontend/dist/", manifest.ID)
		}
		_, backendErr := os.Stat(filepath.Join(pluginRoot, "backend", "main.go"))
		dynamicGo := manifest.Execution != nil && manifest.Execution.Backend != nil && manifest.Execution.Backend.DynamicGo != nil
		backend := backendErr == nil && !dynamicGo
		if backendErr != nil && !errors.Is(backendErr, os.ErrNotExist) {
			return nil, fmt.Errorf("读取插件 %s backend 入口: %w", manifest.ID, backendErr)
		}
		if !backend && frontendEntry == "" {
			continue
		}
		specs = append(specs, packageSpec{id: manifest.ID, backend: backend, frontend: frontendEntry != "", frontendEntry: frontendEntry, frontendServerEntry: frontendServerEntry})
	}
	return specs, nil
}

func pluginManifestVersion(root, pluginID string) (string, error) {
	path := filepath.Join(root, "extensions", "plugins", pluginID, "vastplan.plugin.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取插件 %s 清单: %w", pluginID, err)
	}
	manifest, err := pluginv1.ParseManifest(raw)
	if err != nil {
		return "", fmt.Errorf("解析插件 %s 清单: %w", pluginID, err)
	}
	if manifest.ID != pluginID {
		return "", fmt.Errorf("插件清单 %s 的 id 是 %s", pluginID, manifest.ID)
	}
	return manifest.Version, nil
}

func (r *runtime) writeFixtures() error {
	certFile, keyFile := filepath.Join(r.runDir, "secrets", "tls-cert.pem"), filepath.Join(r.runDir, "secrets", "tls-key.pem")
	if err := writeTLS(certFile, keyFile); err != nil {
		return err
	}
	seedTrust, err := ensureSigningIdentity(filepath.Join(r.runDir, "secrets", "artifact-signing.pem"), "vastplan", "local-development")
	if err != nil {
		return fmt.Errorf("创建本次 Seed 签名身份: %w", err)
	}
	testingTrust, err := ensureSigningIdentity(r.testingRepositorySigningKey(), "vastplan", "local-testing")
	if err != nil {
		return fmt.Errorf("准备持久化测试签名身份: %w", err)
	}
	if err := writeTrustDocument(filepath.Join(r.runDir, "secrets", "artifact-trust.json"), seedTrust, testingTrust); err != nil {
		return err
	}
	if err := writeTrustDocument(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json"), seedTrust); err != nil {
		return err
	}
	if err := writeTrustDocument(r.testingRepositoryTrust(), testingTrust); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-read.token"), []byte("vastplan-local-artifact-reader\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-publish.token"), []byte("vastplan-local-artifact-publisher\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "artifact-bundle.token"), []byte("vastplan-local-artifact-bundle\n"), 0o600); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(r.runDir, "secrets", "vault-token"), []byte("vastplan-local-vault-token\n"), 0o600); err != nil {
		return err
	}
	if err := writeSessions(filepath.Join(r.runDir, "secrets", "portal-sessions.json")); err != nil {
		return err
	}
	if err := writeDevelopmentTransportIdentities(filepath.Join(r.runDir, "secrets")); err != nil {
		return err
	}
	template, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "platform-management-profile.json"))
	if err != nil {
		return err
	}
	portalCatalog, err := os.ReadFile(filepath.Join(r.options.root, "engineering", "deploy", "portal-platform-catalog.json"))
	if err != nil {
		return err
	}
	rendered, err := renderPlatformProfile(template, portalCatalog, r.runDir, r.persistentStateRoot(), r.options.artifactListen)
	if err != nil {
		return err
	}
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
	return r.writeSeedRepositoryProfile()
}

func (r *runtime) writeSeedRepositoryProfile() error {
	profile := fmt.Sprintf("version: 1\nid: seed-repository\nlisten: %s\nrepositoryRoot: %s\ntrustFile: %s\ntlsCertFile: %s\ntlsKeyFile: %s\nreadTokenFile: %s\npublishTokenFile: %s\n",
		yamlString(r.options.seedArtifactListen), yamlString(filepath.Join(r.runDir, "repository")),
		yamlString(filepath.Join(r.runDir, "secrets", "seed-artifact-trust.json")), yamlString(filepath.Join(r.runDir, "secrets", "tls-cert.pem")),
		yamlString(filepath.Join(r.runDir, "secrets", "tls-key.pem")), yamlString(filepath.Join(r.runDir, "secrets", "artifact-read.token")),
		yamlString(filepath.Join(r.runDir, "secrets", "artifact-publish.token")))
	return os.WriteFile(filepath.Join(r.runDir, "seed-repository.yaml"), []byte(profile), 0o600)
}

func yamlString(value string) string {
	raw, _ := json.Marshal(value)
	return string(raw)
}

func renderPlatformProfile(template, portalCatalog []byte, runDir, stateDir, artifactListen string) ([]byte, error) {
	rendered := bytes.ReplaceAll(template, []byte("__VASTPLAN_DEV_ROOT__"), []byte(filepath.ToSlash(runDir)))
	rendered = bytes.ReplaceAll(rendered, []byte("__VASTPLAN_DEV_STATE__"), []byte(filepath.ToSlash(stateDir)))
	rendered = bytes.ReplaceAll(rendered, []byte("__VASTPLAN_ARTIFACT_LISTEN__"), []byte(artifactListen))
	var canonicalCatalog any
	if err := json.Unmarshal(portalCatalog, &canonicalCatalog); err != nil {
		return nil, fmt.Errorf("解析 Portal Platform Catalog: %w", err)
	}
	canonicalRaw, err := json.Marshal(canonicalCatalog)
	if err != nil {
		return nil, err
	}
	quotedCatalog, err := json.Marshal(string(canonicalRaw))
	if err != nil {
		return nil, err
	}
	rendered = bytes.ReplaceAll(rendered, []byte(`"__VASTPLAN_PORTAL_PLATFORM_CATALOG__"`), quotedCatalog)
	profile, err := backendcompositionv1.ParsePlatformProfile(rendered)
	if err != nil {
		return nil, fmt.Errorf("解析开发 Platform Profile 模板: %w", err)
	}
	for index := range profile.Services {
		if profile.Services[index].ID == "platform-database-runtime" {
			// 开发编排器只有一个 local-platform 节点。生产模板保持两个
			// active-active 副本，开发投影显式缩为一个，避免伪造第二节点。
			profile.Services[index].Replicas = 1
			raw, err := json.MarshalIndent(profile, "", "  ")
			if err != nil {
				return nil, err
			}
			return append(raw, '\n'), nil
		}
	}
	return nil, errors.New("开发 Platform Profile 缺少 platform-database-runtime")
}

func (r *runtime) start(ctx context.Context) error {
	kernel := filepath.Join(r.runDir, "dynamic", "backend-kernel")
	if _, err := r.startChild("seed-artifact-server", nil, kernel, "seed-artifact-server", "-profile", filepath.Join(r.runDir, "seed-repository.yaml")); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "https://"+r.options.seedArtifactListen, 30*time.Second, true); err != nil {
		return fmt.Errorf("Seed 制品仓库未就绪: %w", err)
	}
	if err := r.startNATS(); err != nil {
		return err
	}
	if err := r.startVault(); err != nil {
		return err
	}
	env := r.serviceEnv()
	natsURL := "nats://" + r.options.natsListen
	nodeArgs := []string{
		"reconcile", "-nats-url", natsURL, "-nats-allow-insecure", "-nats-bootstrap", "-nats-replicas", "1",
		"-deployment", "platform-management", "-tenant", "local", "-node-id", "local-platform-node",
		"-labels", "environment=local-platform",
		"-runtime-root", filepath.Join(r.runDir, "installed", "backend"), "-actual-state", filepath.Join(r.persistentStateRoot(), "actual-state.json"),
		"-lock", filepath.Join(r.runDir, "state", "node-agent.lock"), "-third-party-plugin-policy", "deny",
		"-publisher-plugin-policies", "vastplan=allow-trusted", "-plugin-placement-default", "process-only",
		"-plugin-placements", "cn.vastplan.foundation.security.bootstrap-policy=require-dynamic-go",
		"-backend-platform-catalog", filepath.Join(r.runDir, "backend-platform-catalog.json"), "-allow-development-plugins",
		"-frontend-delivery-origin", filepath.Join(r.persistentStateRoot(), "frontend-delivery-origin"),
		"-transport-seed", filepath.Join(r.runDir, "secrets", platformNodeTransportSeed),
		"-transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
	}
	nodeArgs = append(nodeArgs, r.managedArtifactSourceArgs()...)
	if _, err := r.startChild("node-agent", env, kernel, nodeArgs...); err != nil {
		return err
	}
	time.Sleep(750 * time.Millisecond)
	platformRevision, err := r.platformManagementRevision()
	if err != nil {
		return err
	}
	controllerArgs := []string{
		"controlplane", "-nats-url", natsURL, "-nats-allow-insecure", "-bootstrap", "-replicas", "1",
		"-platform-profile", filepath.Join(r.runDir, "platform-management-profile.json"),
		"-application-composition", filepath.Join(r.options.root, "engineering", "deploy", "platform-management-application.json"),
		"-deployment-revision", platformRevision, "-repository", filepath.Join(r.runDir, "repository"), "-controller",
		"-backend-platform-catalog", filepath.Join(r.runDir, "backend-platform-catalog.json"),
	}
	controllerArgs = append(controllerArgs, r.controllerArtifactSourceArgs()...)
	if _, err := r.startChild("controller", env, kernel, controllerArgs...); err != nil {
		return err
	}
	if err := waitForUnits(ctx, filepath.Join(r.persistentStateRoot(), "actual-state.json"), 8, 90*time.Second); err != nil {
		return fmt.Errorf("平台 Backend 未收敛: %w", err)
	}
	if err := waitHTTP(ctx, "https://"+r.options.artifactListen, 30*time.Second, true); err != nil {
		return fmt.Errorf("托管测试制品仓库未就绪: %w", err)
	}
	portalArgs := []string{
		filepath.Join(r.options.root, "core", "kernels", "frontend-host", "dist", "portal-host.cjs"),
		"--listen", r.options.portalListen,
		"--tls-cert", filepath.Join(r.runDir, "secrets", "tls-cert.pem"), "--tls-key", filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"--session-file", filepath.Join(r.runDir, "secrets", "portal-sessions.json"),
		"--portal-assets", filepath.Join(r.runDir, "portal-assets"),
		"--frontend-delivery-origin", filepath.Join(r.persistentStateRoot(), "frontend-delivery-origin"),
		"--frontend-delivery-cache", filepath.Join(r.runDir, "frontend-delivery-cache"),
		"--nats-servers", natsURL, "--allow-insecure-nats",
		"--addressing-contracts", filepath.Join(r.options.root, "contracts", "proto"),
		"--transport-seed", filepath.Join(r.runDir, "secrets", portalHostTransportSeed),
		"--transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
		"--composer-logical-service", "platform.portal-composer",
		"--interaction-logical-service", "platform.interaction-broker",
	}
	if _, err := r.startChild("portal-kernel", env, "node", portalArgs...); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "https://"+r.options.portalListen+"/v1/csrf", 45*time.Second, true); err != nil {
		return fmt.Errorf("Node Portal Kernel 未就绪: %w", err)
	}
	if err := publishPortal("https://" + r.options.portalListen); err != nil {
		return fmt.Errorf("发布初始 Portal 组合: %w", err)
	}
	if err := publishManagedService("https://" + r.options.portalListen); err != nil {
		return fmt.Errorf("发布初始在线服务组合: %w", err)
	}
	// Publish the managed deployment before joining its first node. Starting the
	// agent earlier turns the expected initial absence into exponential retries
	// and can add tens of seconds to every local startup.
	managedNodeArgs := []string{
		"reconcile", "-nats-url", natsURL, "-nats-allow-insecure",
		"-deployment", "managed-services", "-tenant", "local", "-node-id", "local-managed-node",
		"-labels", "environment=local-managed",
		"-runtime-root", filepath.Join(r.runDir, "installed", "managed-services"), "-actual-state", filepath.Join(r.persistentStateRoot(), "managed-services-actual.json"),
		"-lock", filepath.Join(r.runDir, "state", "managed-services.lock"), "-third-party-plugin-policy", "deny",
		"-publisher-plugin-policies", "vastplan=allow-trusted", "-plugin-placement-default", "process-only",
		"-transport-seed", filepath.Join(r.runDir, "secrets", managedNodeTransportSeed),
		"-transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
	}
	managedNodeArgs = append(managedNodeArgs, r.managedArtifactSourceArgs()...)
	if _, err := r.startChild("managed-node-agent", env, kernel, managedNodeArgs...); err != nil {
		return err
	}
	if err := waitForUnits(ctx, filepath.Join(r.persistentStateRoot(), "managed-services-actual.json"), 1, 60*time.Second); err != nil {
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

func (r *runtime) platformManagementRevision() (string, error) {
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(r.runDir, "platform-management-profile.json"))
	if err != nil {
		return "", err
	}
	if profile.Revision == 0 {
		return "", errors.New("开发 Platform Profile revision 必须大于 0")
	}
	return fmt.Sprint(profile.Revision), nil
}

func (r *runtime) serviceEnv() map[string]string {
	return map[string]string{
		"VASTPLAN_CREDENTIALS_STATE_FILE":          filepath.Join(r.persistentStateRoot(), "credentials.json"),
		"VASTPLAN_VAULT_ADDR":                      "http://" + r.options.vaultListen,
		"VASTPLAN_VAULT_TRANSIT_KEY":               "vastplan-local",
		"VASTPLAN_VAULT_TOKEN_FILE":                filepath.Join(r.runDir, "secrets", "vault-token"),
		"VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE": filepath.Join(r.persistentStateRoot(), "database-connections.json"),
		"VASTPLAN_DEPLOYMENT_MANAGER_STATE_FILE":   filepath.Join(r.persistentStateRoot(), "deployment-manager.json"),
		"VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT":     r.testingRepositoryVolumes(),
		"VASTPLAN_ARTIFACT_REPOSITORY":             r.testingRepositoryData(),
		"VASTPLAN_ARTIFACT_TRUST":                  r.testingRepositoryTrust(),
		"VASTPLAN_ARTIFACT_TLS_CERT":               filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
		"VASTPLAN_ARTIFACT_TLS_KEY":                filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"VASTPLAN_ARTIFACT_READ_TOKEN":             "vastplan-local-artifact-reader",
		"VASTPLAN_ARTIFACT_PUBLISH_TOKEN":          "vastplan-local-artifact-publisher",
		"VASTPLAN_ARTIFACT_BUNDLE_TOKEN":           "vastplan-local-artifact-bundle",
		"VASTPLAN_ARTIFACT_MIGRATION_STATE":        filepath.Join(r.testingRepositoryRoot(), "control", "repository-migration.json"),
		"VASTPLAN_DYNAMIC_GO_HOST":                 filepath.Join(r.runDir, "dynamic", "vastplan-go-dynamic-host"),
	}
}

// persistentStateRoot holds governed plugin state across ordinary platformdev
// restarts. Permanent artifact-reference snapshots use monotonic generations,
// so resetting their producer state while retaining the repository would make
// a healthy restart look like a stale writer. `platform-dev.sh clean` and
// `--fresh` still remove this entire development state root intentionally.
func (r *runtime) persistentStateRoot() string {
	return filepath.Join(r.options.stateRoot, "state")
}

func (r *runtime) testingRepositoryRoot() string {
	return filepath.Join(r.options.stateRoot, "repositories", "testing")
}

func (r *runtime) testingRepositoryVolumes() string {
	return filepath.Join(r.testingRepositoryRoot(), "volumes")
}

func (r *runtime) testingRepositoryData() string {
	return filepath.Join(r.testingRepositoryVolumes(), "repository.primary")
}

func (r *runtime) testingRepositorySecrets() string {
	return filepath.Join(r.testingRepositoryRoot(), "secrets")
}

func (r *runtime) testingRepositorySigningKey() string {
	return filepath.Join(r.testingRepositorySecrets(), "artifact-signing.pem")
}

func (r *runtime) testingRepositoryTrust() string {
	return filepath.Join(r.testingRepositoryRoot(), "artifact-trust.json")
}

func (r *runtime) managedArtifactSourceArgs() []string {
	return []string{
		"-bootstrap-repository", filepath.Join(r.runDir, "repository"),
		"-bootstrap-inventory", filepath.Join(r.runDir, "seed-inventory.json"),
		"-bootstrap-upgrade",
		"-repository-url", "https://" + r.options.artifactListen,
		"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		"-repository-ca", filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
	}
}

func (r *runtime) controllerArtifactSourceArgs() []string {
	return []string{
		"-repository-url", "https://" + r.options.artifactListen,
		"-repository-trust", filepath.Join(r.runDir, "secrets", "artifact-trust.json"),
		"-repository-ca", filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
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
		"repositories": map[string]any{
			"seed":    map[string]any{"url": "https://" + r.options.seedArtifactListen, "persistent": false},
			"testing": map[string]any{"url": "https://" + r.options.artifactListen, "persistent": true},
		},
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
				"plugins":   []any{map[string]string{"id": "cn.vastplan.hello-world", "version": "0.1.0", "channel": "stable"}},
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

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("%s 必须是普通目录且不能是符号链接", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s 权限过宽 %o，要求 0700 或更严格", path, info.Mode().Perm())
	}
	return nil
}

func ensureSigningIdentity(privateFilename, publisher, keyID string) (pluginservice.TrustKey, error) {
	if strings.TrimSpace(publisher) == "" || strings.TrimSpace(keyID) == "" {
		return pluginservice.TrustKey{}, errors.New("签名身份 publisher 和 keyId 不能为空")
	}
	if err := ensurePrivateDirectory(filepath.Dir(privateFilename)); err != nil {
		return pluginservice.TrustKey{}, err
	}
	info, err := os.Lstat(privateFilename)
	if errors.Is(err, os.ErrNotExist) {
		_, privateKey, generateErr := ed25519.GenerateKey(rand.Reader)
		if generateErr != nil {
			return pluginservice.TrustKey{}, generateErr
		}
		encoded, marshalErr := pluginservice.MarshalEd25519PrivateKeyPEM(privateKey)
		if marshalErr != nil {
			return pluginservice.TrustKey{}, marshalErr
		}
		file, createErr := os.OpenFile(privateFilename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if createErr == nil {
			written, writeErr := file.Write(encoded)
			if writeErr == nil && written != len(encoded) {
				writeErr = io.ErrShortWrite
			}
			syncErr := file.Sync()
			closeErr := file.Close()
			if writeErr != nil || syncErr != nil || closeErr != nil {
				_ = os.Remove(privateFilename)
				return pluginservice.TrustKey{}, errors.Join(writeErr, syncErr, closeErr)
			}
		} else if !errors.Is(createErr, os.ErrExist) {
			return pluginservice.TrustKey{}, createErr
		}
		info, err = os.Lstat(privateFilename)
	}
	if err != nil {
		return pluginservice.TrustKey{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return pluginservice.TrustKey{}, fmt.Errorf("签名私钥 %s 必须是普通文件且不能是符号链接", privateFilename)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return pluginservice.TrustKey{}, fmt.Errorf("签名私钥 %s 权限过宽 %o，要求 0600 或更严格", privateFilename, info.Mode().Perm())
	}
	privateKey, err := pluginservice.LoadEd25519PrivateKeyPEM(privateFilename)
	if err != nil {
		return pluginservice.TrustKey{}, err
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return pluginservice.TrustKey{}, errors.New("签名私钥无法导出 Ed25519 公钥")
	}
	return pluginservice.TrustKey{
		Publisher: publisher, KeyID: keyID, PublicKey: base64.StdEncoding.EncodeToString(publicKey),
	}, nil
}

func writeTrustDocument(filename string, keys ...pluginservice.TrustKey) error {
	document := pluginservice.TrustDocumentForPublicKeys(keys...)
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
