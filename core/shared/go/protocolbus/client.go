package protocolbus

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/processguard"
)

type currentDispatchTargetKey struct{}

type processLogWriter struct {
	logf   func(string, ...any)
	prefix string
}

// ManagedLaunchSpec starts one logical execution unit inside a Runtime Host
// process that is owned outside protocolbus. Start receives the exact trusted
// environment for this plugin session. Stop must be idempotent and release only
// this unit (and its pool lease), never an unrelated unit in the same process.
type ManagedLaunchSpec struct {
	PID         int
	RuntimeKind string
	Start       func(environment []string) error
	Stop        func()
	// Done reports that the managed unit or its physical host exited before the
	// protocol handshake completed. Nil disables the early failure path.
	Done <-chan error
}

func (w processLogWriter) Write(raw []byte) (int, error) {
	const maxLine = 64 << 10
	line := strings.TrimSpace(string(raw))
	if len(line) > maxLine {
		line = line[:maxLine] + "…[truncated]"
	}
	if line != "" && w.logf != nil {
		w.logf("%s %s", w.prefix, line)
	}
	return len(raw), nil
}

// Launch 拉起插件进程并等待它完成回连、握手、贡献注册与激活（§2.2）。
//
// 宿主注入连接端点 + magic cookie + 一次性 launch token；插件回连本宿主。
// 握手失败（magic/版本/engines）的原因经 launch token 回传，故此处能给出确切错误。
func (h *Host) Launch(ctx context.Context, binPath string) (*PluginInstance, error) {
	return h.LaunchWithPolicy(ctx, binPath, LaunchPolicy{UnrestrictedContext: true})
}

// LaunchWithPolicy 启动插件，并把已验签清单中的身份、贡献和内核服务依赖绑定到
// 一次性 launch token。空 Policy 只用于本地演示/兼容夹具，但仍强制 token 认证。
func (h *Host) LaunchWithPolicy(ctx context.Context, binPath string, policy LaunchPolicy) (*PluginInstance, error) {
	return h.LaunchSpecWithPolicy(ctx, LaunchSpec{Command: binPath}, policy)
}

// LaunchSpecWithPolicy 通过运行驱动生成的无 shell 规格启动插件。
func (h *Host) LaunchSpecWithPolicy(ctx context.Context, spec LaunchSpec, policy LaunchPolicy) (*PluginInstance, error) {
	if h.addr == "" {
		return nil, errors.New("宿主尚未 Start，插件无处回连")
	}
	if strings.TrimSpace(spec.Command) == "" {
		return nil, errors.New("插件启动命令不能为空")
	}
	if err := validateStartupConfiguration(policy.Configuration); err != nil {
		return nil, err
	}
	if err := validateAutonomousPolicy(policy); err != nil {
		return nil, err
	}
	if _, err := compileCapabilityGrantPlan(policy.KernelServices); err != nil {
		return nil, fmt.Errorf("编译 Capability Grant Plan: %w", err)
	}
	if err := validateExtraEnvironment(spec.ExtraEnv); err != nil {
		return nil, err
	}

	token := newToken()
	resultCh := make(chan launchResult, 1)
	h.mu.Lock()
	h.launches[token] = &launchAttempt{result: resultCh, policy: cloneLaunchPolicy(policy)}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.launches, token)
		h.mu.Unlock()
	}()

	cmd := exec.Command(spec.Command, spec.Args...)
	cmd.Dir = spec.Dir
	environmentAllowlist := append([]string(nil), h.PluginEnvironmentAllowlist...)
	environmentAllowlist = append(environmentAllowlist, policy.EnvironmentAllowlist...)
	cmd.Env = append(pluginEnvironment(environmentAllowlist), spec.ExtraEnv...)
	cmd.Env = append(cmd.Env,
		protocol.MagicEnvKey+"="+protocol.MagicCookie,
		protocol.HostAddrEnvKey+"="+h.addr,
		protocol.LaunchTokenEnvKey+"="+token,
	)
	if runtimeAudience, err := runtimeAudienceEnvironment(policy); err == nil {
		cmd.Env = append(cmd.Env, runtimeAudience)
	}
	if len(policy.Configuration) != 0 {
		cmd.Env = append(cmd.Env, protocol.PluginConfigEnvKey+"="+string(policy.Configuration))
	}
	logID := policy.PluginID
	if logID == "" {
		logID = filepath.Base(spec.Command)
	}
	cmd.Stdout = processLogWriter{logf: h.Logf, prefix: "plugin=" + logID + " stream=stdout"}
	cmd.Stderr = processLogWriter{logf: h.Logf, prefix: "plugin=" + logID + " stream=stderr"}
	guardian := h.ProcessGuardian
	if guardian == nil {
		guardian = processguard.Default()
	}
	if err := guardian.Prepare(cmd); err != nil {
		return nil, fmt.Errorf("准备插件进程守护: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("拉起插件进程: %w", err)
	}
	h.Logf("插件进程已启动 pid=%d，等待回连 %s", cmd.Process.Pid, h.addr)

	// 进程提前退出（如 magic 不符自杀）时立刻脱身，不必等满超时
	exited := make(chan error, 1)
	processDone := make(chan struct{})
	go func() {
		err := cmd.Wait()
		close(processDone)
		exited <- err
	}()

	kill := func() {
		_ = guardian.Kill(cmd)
	}

	select {
	case res := <-resultCh:
		if res.err != nil {
			kill()
			return nil, res.err
		}
		if !res.sess.bindProcess(cmd, processDone, guardian) {
			kill()
			return nil, fmt.Errorf("插件完成接入后立即失联: %w", res.sess.err())
		}
		return &PluginInstance{
			PluginID:        res.sess.pluginID,
			Version:         res.sess.pluginVersion,
			SessionID:       res.sess.id,
			PID:             cmd.Process.Pid,
			RuntimeAudience: launchRuntimeAudience(res.sess.policy),
			runtimeKind:     spec.RuntimeKind,
			session:         res.sess,
		}, nil

	case err := <-exited:
		// 进程没连上就退了；若握手已记录原因，resultCh 里会有更准确的错误
		select {
		case res := <-resultCh:
			if res.err != nil {
				return nil, res.err
			}
		default:
		}
		return nil, fmt.Errorf("插件进程未完成接入即退出: %v", err)

	case <-time.After(h.launchTimeout()):
		kill()
		return nil, fmt.Errorf("等待插件接入超时（%v）", h.launchTimeout())

	case <-ctx.Done():
		kill()
		return nil, ctx.Err()
	}
}

// LaunchManagedWithPolicy attaches a logical plugin unit started by a shared
// Runtime Host to the normal one-time-ticket handshake. The resulting session
// follows exactly the same authorization, contribution and lifecycle pipeline
// as an independent process, while its PID denotes the shared physical host.
func (h *Host) LaunchManagedWithPolicy(ctx context.Context, spec ManagedLaunchSpec,
	policy LaunchPolicy) (*PluginInstance, error) {
	if h.addr == "" {
		return nil, errors.New("宿主尚未 Start，插件无处回连")
	}
	if spec.Start == nil || spec.Stop == nil {
		return nil, errors.New("托管执行单元必须提供 Start 和 Stop")
	}
	if err := validateStartupConfiguration(policy.Configuration); err != nil {
		return nil, err
	}
	if err := validateAutonomousPolicy(policy); err != nil {
		return nil, err
	}
	if _, err := compileCapabilityGrantPlan(policy.KernelServices); err != nil {
		return nil, fmt.Errorf("编译 Capability Grant Plan: %w", err)
	}
	token := newToken()
	resultCh := make(chan launchResult, 1)
	h.mu.Lock()
	h.launches[token] = &launchAttempt{result: resultCh, policy: cloneLaunchPolicy(policy)}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.launches, token)
		h.mu.Unlock()
	}()

	environmentAllowlist := append([]string(nil), h.PluginEnvironmentAllowlist...)
	environmentAllowlist = append(environmentAllowlist, policy.EnvironmentAllowlist...)
	environment := pluginEnvironment(environmentAllowlist)
	environment = append(environment,
		protocol.MagicEnvKey+"="+protocol.MagicCookie,
		protocol.HostAddrEnvKey+"="+h.addr,
		protocol.LaunchTokenEnvKey+"="+token,
	)
	if runtimeAudience, err := runtimeAudienceEnvironment(policy); err == nil {
		environment = append(environment, runtimeAudience)
	}
	if len(policy.Configuration) != 0 {
		environment = append(environment, protocol.PluginConfigEnvKey+"="+string(policy.Configuration))
	}
	if err := spec.Start(environment); err != nil {
		spec.Stop()
		return nil, fmt.Errorf("启动托管插件执行单元: %w", err)
	}
	stop := spec.Stop
	failed := true
	defer func() {
		if failed {
			stop()
		}
	}()

	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		if !res.sess.bindManagedUnit(stop) {
			return nil, fmt.Errorf("托管插件完成接入后立即失联: %w", res.sess.err())
		}
		failed = false
		return &PluginInstance{
			PluginID: res.sess.pluginID, Version: res.sess.pluginVersion,
			SessionID: res.sess.id, PID: spec.PID,
			RuntimeAudience: launchRuntimeAudience(res.sess.policy),
			runtimeKind:     spec.RuntimeKind, session: res.sess,
		}, nil
	case <-time.After(h.launchTimeout()):
		return nil, fmt.Errorf("等待托管插件接入超时（%v）", h.launchTimeout())
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-spec.Done:
		if err == nil {
			err = errors.New("托管插件执行单元已退出")
		}
		return nil, err
	}
}

func launchRuntimeAudience(policy LaunchPolicy) string {
	audience, err := runtimeAudience(policy)
	if err != nil {
		return ""
	}
	return audience
}

func cloneLaunchPolicy(policy LaunchPolicy) LaunchPolicy {
	policy.Contributions = append([]pluginv1.RuntimeContribution(nil), policy.Contributions...)
	policy.KernelServices = append([]string(nil), policy.KernelServices...)
	policy.ContextAccess.Required = append([]string(nil), policy.ContextAccess.Required...)
	policy.ContextAccess.Optional = append([]string(nil), policy.ContextAccess.Optional...)
	policy.ContextAccess.Baggage = append([]string(nil), policy.ContextAccess.Baggage...)
	policy.ContextCeiling = append([]string(nil), policy.ContextCeiling...)
	policy.EnvironmentAllowlist = append([]string(nil), policy.EnvironmentAllowlist...)
	policy.Configuration = append([]byte(nil), policy.Configuration...)
	policy.RequiredFeatures = append([]string(nil), policy.RequiredFeatures...)
	return policy
}

func validateStartupConfiguration(raw []byte) error {
	if len(raw) == 0 {
		return nil
	}
	if len(raw) > protocol.MaxPluginConfigBytes || !json.Valid(raw) {
		return errors.New("插件启动配置必须是最大 64KiB 的合法 JSON")
	}
	var object map[string]any
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return errors.New("插件启动配置根必须是 JSON object")
	}
	return nil
}

func validateAutonomousPolicy(policy LaunchPolicy) error {
	tenantID := policy.AutonomousTenantID
	if policy.BackgroundService != (tenantID != "") {
		return errors.New("后台服务启动策略必须同时声明能力并绑定租户")
	}
	if tenantID != "" && (strings.TrimSpace(tenantID) != tenantID || len(tenantID) > 160) {
		return errors.New("后台服务启动策略 tenantId 无效")
	}
	return nil
}

func validateExtraEnvironment(environment []string) error {
	for _, item := range environment {
		key, _, ok := splitEnvironment(item)
		if !ok || key == "" {
			return errors.New("运行驱动 ExtraEnv 必须使用 KEY=VALUE")
		}
		if reservedPluginEnvironmentKey(key) {
			return fmt.Errorf("运行驱动不得覆盖宿主保留环境变量 %s", key)
		}
	}
	return nil
}

func reservedPluginEnvironmentKey(key string) bool {
	switch key {
	case protocol.MagicEnvKey, protocol.HostAddrEnvKey, protocol.LaunchTokenEnvKey, protocol.PluginConfigEnvKey, protocol.RuntimeAudienceEnvKey:
		return true
	default:
		return false
	}
}

func pluginEnvironment(allowlist []string) []string {
	allowed := append([]string(nil), allowlist...)
	// Windows 进程创建和部分系统库依赖这两个环境变量；它们不承载应用秘密。
	if runtime.GOOS == "windows" {
		allowed = append(allowed, "SystemRoot", "WINDIR")
	}
	sort.Strings(allowed)
	out := make([]string, 0, len(allowed))
	last := ""
	for _, key := range allowed {
		if key == "" || key == last || reservedPluginEnvironmentKey(key) {
			continue
		}
		last = key
		if value, ok := os.LookupEnv(key); ok {
			out = append(out, key+"="+value)
		}
	}
	return out
}

func splitEnvironment(item string) (string, string, bool) {
	index := strings.IndexByte(item, '=')
	if index <= 0 {
		return "", "", false
	}
	return item[:index], item[index+1:], true
}

// Invoke 扩展点被触发时的**公开入口**，是完整的调用管道：
//
//	before 钩子（可一票否决）→ 权限判定 → 分发 → after 钩子（只观察）
//
// 权限按 select 语义走 permission.checker，零校验器 → fail-closed 拒绝（ADR-0021）。
// 钩子按 fanout 语义顺序执行，承载限流/配额/计量等横切关注点（皆为插件）。
// 未获放行/被否决均返回**应用层错误**（非传输层——工程规范 §4.2）。
