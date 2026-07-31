// Command platformdev assembles and runs the complete local platform-management
// stack. It is development-only orchestration: production keeps external NATS,
// Vault Transit, signed artifacts, TLS identities, and systemd-managed agents.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	recoveryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/recovery/v1"
	sharedcontrolplane "cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
)

const (
	devAdminToken  = "vastplan-local-platform-admin"
	authorToken    = "vastplan-local-portal-author"
	approverToken  = "vastplan-local-portal-approver"
	publisherToken = "vastplan-local-portal-publisher"
)

type options struct {
	root, stateRoot                                                                                   string
	listen, portalListen, artifactListen, seedArtifactListen, vaultListen, recoveryListen, natsListen string
	artifactProtocol                                                                                  string
	hot                                                                                               bool
	detach                                                                                            bool
	applyPlatform                                                                                     bool
	rebuildSeed                                                                                       bool
}

type child struct {
	name string
	cmd  *exec.Cmd
	done chan struct{}
	mu   sync.RWMutex
	err  error
}

type runtime struct {
	options               options
	runDir                string
	nats                  *natsserver.Server
	vault                 *http.Server
	proxy                 *http.Server
	children              []*child
	mu                    sync.RWMutex
	ready                 bool
	hmr                   *frontendHMR
	backendInputDigest    string
	repositoryProfile     artifactrepositoryv1.Profile
	seedArtifacts         seedArtifactSelection
	seedSnapshotCandidate string
	seedSnapshotMigration bool
	seedHostRefresh       bool
	recovery              recoveryStatus
}

type packageSpec struct {
	id, backendEntry, frontendEntry, frontendServerEntry string
	backend, nodeBackend, frontend                       bool
}

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	var opts options
	flag.StringVar(&opts.root, "root", "", "VastPlan repository root")
	flag.StringVar(&opts.stateRoot, "state-root", ".vastplan/dev-platform", "development runtime root")
	flag.StringVar(&opts.listen, "listen", "127.0.0.1:18080", "developer gateway address")
	flag.StringVar(&opts.portalListen, "portal-listen", "127.0.0.1:18444", "internal Node Portal Kernel address")
	flag.StringVar(&opts.artifactListen, "artifact-listen", "127.0.0.1:18443", "internal artifact repository address")
	flag.StringVar(&opts.artifactProtocol, "artifact-protocol", "local-test", "development repository protocol: local-test or remote-compat")
	flag.StringVar(&opts.seedArtifactListen, "seed-artifact-listen", "127.0.0.1:18442", "seed artifact repository address")
	flag.StringVar(&opts.vaultListen, "vault-listen", "127.0.0.1:18200", "development Vault Transit stub address")
	flag.StringVar(&opts.recoveryListen, "recovery-listen", "127.0.0.1:18441", "internal Kernel Recovery status address")
	flag.StringVar(&opts.natsListen, "nats-listen", "127.0.0.1:0", "development NATS address; port 0 chooses a free port")
	flag.BoolVar(&opts.hot, "hot", true, "enable transactional frontend plugin hot replacement")
	flag.BoolVar(&opts.detach, "detach", false, "detach background runtime from the launching terminal session")
	flag.BoolVar(&opts.applyPlatform, "apply-platform", false, "explicitly publish the development platform baseline")
	flag.BoolVar(&opts.rebuildSeed, "rebuild-seed", false, "explicitly rebuild and promote the stable development Seed Runtime")
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
	if opts.artifactProtocol != "local-test" && opts.artifactProtocol != "remote-compat" {
		return errors.New("-artifact-protocol 只允许 local-test 或 remote-compat")
	}
	if opts.rebuildSeed && !opts.applyPlatform {
		return errors.New("-rebuild-seed 只能与显式 -apply-platform 一起使用")
	}
	if opts.detach {
		if err := detachManagedRuntime(); err != nil {
			return fmt.Errorf("脱离启动终端会话: %w", err)
		}
	}
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

	log.Printf("前后端最小内核已就绪: http://%s/operations", opts.listen)
	if !opts.applyPlatform {
		log.Printf("本次启动未执行任何 Deployment、Portal Activation 或业务服务发布")
	}
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
	for _, dir := range []string{r.persistentStateRoot(), r.testingRepositoryRoot(), r.testingRepositoryVolumes(), r.testingRepositorySecrets(), r.nodeBootstrapCredentialRoot()} {
		if err := ensurePrivateDirectory(dir); err != nil {
			return fmt.Errorf("准备持久化开发目录: %w", err)
		}
	}
	log.Printf("[1/6] 生成仅限本地开发的 TLS、session、Seed 仓库配置与签名身份")
	if err := r.writeFixtures(ctx); err != nil {
		return err
	}
	selection, err := r.seedSelection()
	if err != nil {
		return fmt.Errorf("解析最小 Seed 制品计划: %w", err)
	}
	if !r.options.rebuildSeed {
		refs, restored, err := r.restoreOrMigrateSeedRuntimeSnapshot()
		if err != nil {
			return fmt.Errorf("恢复 Last-Known-Good Seed Runtime: %w", err)
		}
		if restored {
			if r.options.applyPlatform {
				if err := validateExactSeedRefs("Bootstrap Profile", selection.references(), refs); err != nil {
					return fmt.Errorf("Bootstrap Profile 已引用新的 stable 版本，请显式使用 bootstrap --rebuild-seed: %w", err)
				}
				log.Printf("Bootstrap Profile 的精确引用未变化，复用 stable LKG；源码调试应通过 workspace Test Release")
			}
			refreshed, err := r.refreshDevelopmentBackendKernel(ctx)
			if err != nil {
				return fmt.Errorf("刷新开发态 Backend Kernel 宿主: %w", err)
			}
			r.seedHostRefresh = refreshed
			if refreshed {
				log.Printf("已刷新与当前编排器配对的 Backend Kernel；stable 插件仓库保持不变")
			}
			if err := r.signPackageRepository(refs); err != nil {
				return err
			}
			if r.seedSnapshotMigration || r.seedHostRefresh {
				source := "development-host-refresh"
				if r.seedSnapshotMigration {
					source = "recovery-capsule-v1-migration"
				}
				return r.stageSeedRuntimeSnapshot(r.runDir, source)
			}
			return nil
		}
		if !r.options.applyPlatform {
			log.Printf("尚无可复用的 Seed Runtime 快照，本次普通启动执行一次安全初始化构建")
		}
	} else {
		log.Printf("已显式请求重建 stable Seed Runtime")
	}
	log.Printf("最小 Seed 制品计划已确定: %d 个精确插件引用", len(selection.refs))
	if err := r.prepareCachedBuilds(ctx); err != nil {
		return err
	}
	if err := r.signPackageRepository(selection.references()); err != nil {
		return err
	}
	source := "bootstrap-build"
	if !r.options.applyPlatform {
		source = "initial-start-build"
	}
	return r.stageSeedRuntimeSnapshot(r.runDir, source)
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
		"-recovery-capsule", filepath.Join(r.runDir, recoveryCapsuleFilename),
		"-recovery-status", filepath.Join(r.persistentStateRoot(), "recovery-status.json"),
		"-recovery-listen", r.options.recoveryListen,
	}
	nodeArgs = append(nodeArgs, r.nodeBootstrapCredentialArgs()...)
	nodeArgs = append(nodeArgs, r.managedArtifactSourceArgs()...)
	nodeArgs = append(nodeArgs, "-bootstrap-upgrade", "-publish-bootstrap-references")
	platformNodeStartedAt := time.Now().UTC()
	if _, err := r.startChild("node-agent", env, kernel, nodeArgs...); err != nil {
		return err
	}
	time.Sleep(750 * time.Millisecond)
	platformRevision, _, err := r.platformManagementDeployment()
	if err != nil {
		return err
	}
	controllerArgs := []string{
		"controlplane", "-nats-url", natsURL, "-nats-allow-insecure",
		"-key", sharedcontrolplane.DeploymentKey("local", "platform-management"),
		"-repository", filepath.Join(r.runDir, "repository"), "-controller",
		"-backend-platform-catalog", filepath.Join(r.runDir, "backend-platform-catalog.json"),
	}
	if r.options.applyPlatform {
		controllerArgs = append(controllerArgs,
			"-bootstrap", "-replicas", "1",
			"-platform-profile", filepath.Join(r.runDir, "platform-management-profile.json"),
			"-application-composition", filepath.Join(r.options.root, "engineering", "deploy", "platform-management-application.json"),
			"-deployment-revision", platformRevision, "-allow-development-plugins",
		)
	}
	controllerArgs = append(controllerArgs, r.controllerArtifactSourceArgs()...)
	if _, err := r.startChild("controller", env, kernel, controllerArgs...); err != nil {
		return err
	}
	if err := r.startRecoveryMonitor(ctx, platformNodeStartedAt); err != nil {
		return fmt.Errorf("启动 Seed Recovery Capsule 观察器: %w", err)
	}
	if r.options.applyPlatform {
		if err := r.waitForRecoveryStage(ctx, recoveryv1.StageRecovery, platformNodeStartedAt, 120*time.Second); err != nil {
			return fmt.Errorf("显式发布的平台 Recovery 阶段未收敛: %w", err)
		}
	}
	portalArgs := []string{
		filepath.Join(r.options.root, "core", "kernels", "frontend-host", "dist", "portal-host.cjs"),
		"--listen", r.options.portalListen,
		"--tls-cert", filepath.Join(r.runDir, "secrets", "tls-cert.pem"), "--tls-key", filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"--session-file", filepath.Join(r.runDir, "secrets", "portal-sessions.json"),
		"--portal-assets", filepath.Join(r.runDir, "portal-assets"),
		"--access-profile-catalog", filepath.Join(r.runDir, "access-profile-catalog.json"),
		"--api-contract-catalog", filepath.Join(r.persistentStateRoot(), "api-contract-catalog.json"),
		"--frontend-delivery-origin", filepath.Join(r.persistentStateRoot(), "frontend-delivery-origin"),
		"--frontend-delivery-cache", filepath.Join(r.runDir, "frontend-delivery-cache"),
		"--nats-servers", natsURL, "--allow-insecure-nats",
		"--addressing-contracts", filepath.Join(r.options.root, "contracts", "proto"),
		"--transport-seed", filepath.Join(r.runDir, "secrets", portalHostTransportSeed),
		"--transport-trust", filepath.Join(r.runDir, "secrets", transportTrustDocument),
		"--composer-logical-service", "platform.portal-composer",
		"--interaction-logical-service", "platform.interaction-broker",
		"--kernel-recovery-url", "http://" + r.options.recoveryListen,
	}
	portalArgs, err = appendPublishedAPIExposureCatalog(portalArgs, filepath.Join(r.persistentStateRoot(), "api-exposure-gateway.json"))
	if err != nil {
		return err
	}
	if _, err := r.startChild("portal-kernel", env, "node", portalArgs...); err != nil {
		return err
	}
	if err := waitHTTP(ctx, "https://"+r.options.portalListen+"/v1/csrf", 45*time.Second, true); err != nil {
		return fmt.Errorf("Node Portal Kernel 未就绪: %w", err)
	}
	if r.options.applyPlatform {
		if err := r.waitForRecoveryStage(ctx, recoveryv1.StageControlPlane, platformNodeStartedAt, 120*time.Second); err != nil {
			return fmt.Errorf("显式发布的平台控制面未收敛: %w", err)
		}
		if err := publishPortal("https://"+r.options.portalListen,
			filepath.Join(r.options.root, "engineering", "deploy", "portal-application-composition.json")); err != nil {
			return fmt.Errorf("显式发布初始 Portal 组合: %w", err)
		}
		if err := r.waitForRecoveryStage(ctx, recoveryv1.StagePlatform, platformNodeStartedAt, 120*time.Second); err != nil {
			return fmt.Errorf("显式发布的平台完整能力未收敛: %w", err)
		}
	}
	// Business deployments are never published by startup. This agent may join
	// before the first explicit publication and remains in a quiet waiting state.
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
	if r.options.hot {
		if err := r.startFrontendHMR(ctx); err != nil {
			return err
		}
	}
	if err := r.startProxy(); err != nil {
		return err
	}
	if err := r.commitSeedRuntimeSnapshot(); err != nil {
		return fmt.Errorf("提交已收敛的 Seed Runtime 快照: %w", err)
	}
	r.mu.Lock()
	r.ready = true
	r.mu.Unlock()
	return nil
}

func (r *runtime) platformManagementDeployment() (string, int, error) {
	profile, err := backendcompositionv1.ParsePlatformProfileFile(filepath.Join(r.runDir, "platform-management-profile.json"))
	if err != nil {
		return "", 0, err
	}
	if profile.Revision == 0 {
		return "", 0, errors.New("开发 Platform Profile revision 必须大于 0")
	}
	activeUnits := 0
	for _, service := range profile.Services {
		if service.Enabled {
			activeUnits++
		}
	}
	if activeUnits == 0 {
		return "", 0, errors.New("开发 Platform Profile 必须至少包含一个启用的 service unit")
	}
	return fmt.Sprint(profile.Revision), activeUnits, nil
}

func (r *runtime) serviceEnv() map[string]string {
	authorizationRoot := filepath.Join(r.persistentStateRoot(), "authorization")
	return map[string]string{
		"VASTPLAN_VAULT_ADDR":                       "http://" + r.options.vaultListen,
		"VASTPLAN_VAULT_TRANSIT_KEY":                "vastplan-local",
		"VASTPLAN_VAULT_TOKEN_FILE":                 filepath.Join(r.runDir, "secrets", "vault-token"),
		"VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE":  filepath.Join(r.persistentStateRoot(), "database-connections.json"),
		"VASTPLAN_ARTIFACT_FILE_PROVIDER_ROOT":      r.testingRepositoryVolumes(),
		"VASTPLAN_ARTIFACT_REPOSITORY":              r.testingRepositoryData(),
		"VASTPLAN_ARTIFACT_TRUST":                   r.testingRepositoryTrust(),
		"VASTPLAN_ARTIFACT_TLS_CERT":                filepath.Join(r.runDir, "secrets", "tls-cert.pem"),
		"VASTPLAN_ARTIFACT_TLS_KEY":                 filepath.Join(r.runDir, "secrets", "tls-key.pem"),
		"VASTPLAN_ARTIFACT_READ_TOKEN":              "vastplan-local-artifact-reader",
		"VASTPLAN_ARTIFACT_PUBLISH_TOKEN":           "vastplan-local-artifact-publisher",
		"VASTPLAN_ARTIFACT_BUNDLE_TOKEN":            "vastplan-local-artifact-bundle",
		"VASTPLAN_ARTIFACT_ASSESSMENT_TOKEN":        "vastplan-local-artifact-assessment",
		"VASTPLAN_ARTIFACT_ASSESSMENT_REPORTS":      r.testingAssessmentReports(),
		"VASTPLAN_ARTIFACT_MIGRATION_STATE":         filepath.Join(r.testingRepositoryRoot(), "control", "repository-migration.json"),
		"VASTPLAN_ARTIFACT_LOCAL_TOKEN_FILE":        r.testingRepositoryTokenFile(),
		"VASTPLAN_DYNAMIC_GO_HOST":                  filepath.Join(r.runDir, "dynamic", "vastplan-go-dynamic-host"),
		"VASTPLAN_AUTHORIZATION_PERMISSION_CATALOG": filepath.Join(authorizationRoot, "permission-catalog.json"),
		// Seed Runtime Snapshot v1 can restore the last healthy pre-Shared-State
		// policy plugin and Assignment. Keep its exact host contract until every
		// v1 snapshot has been replaced; the old Assignment allowlist prevents
		// this alias from reaching the new plugin generation.
		"VASTPLAN_AUTHORIZATION_POLICY_STATE":           filepath.Join(authorizationRoot, "policy-state.json"),
		"VASTPLAN_AUTHORIZATION_POLICY_BOOTSTRAP_STATE": filepath.Join(authorizationRoot, "policy-state.json"),
		"VASTPLAN_AUTHORIZATION_POLICY_KEY":             filepath.Join(authorizationRoot, "policy-key.json"),
		"VASTPLAN_AUTHORIZATION_POLICY_SNAPSHOT":        filepath.Join(authorizationRoot, "policy-snapshot.json"),
		"VASTPLAN_AUTHORIZATION_POLICY_TRUST":           filepath.Join(authorizationRoot, "policy-trust.json"),
		"VASTPLAN_AUTHORIZATION_POLICY_AUDIENCE":        developmentAuthorizationAudience,
		"VASTPLAN_AUTHORIZATION_DIRECTORY_GROUPS":       filepath.Join(authorizationRoot, "directory-groups.json"),
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

func (r *runtime) nodeBootstrapCredentialRoot() string {
	return filepath.Join(r.persistentStateRoot(), "node-bootstrap-credentials")
}

func (r *runtime) nodeBootstrapCredentialArgs() []string {
	return []string{"-credential-root", r.nodeBootstrapCredentialRoot()}
}

func (r *runtime) startChild(name string, env map[string]string, executable string, args ...string) (*child, error) {
	cmd := exec.Command(executable, args...)
	configureManagedChild(cmd)
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
