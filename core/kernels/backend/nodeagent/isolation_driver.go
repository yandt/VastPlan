package nodeagent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

const (
	processSandboxHostEnv = "VASTPLAN_PROCESS_SANDBOX_HOST"
	containerHostEnv      = "VASTPLAN_CONTAINER_HOST"
	wasmComponentHostEnv  = "VASTPLAN_WASM_COMPONENT_HOST"
)

// IsolationExecutionDriver delegates execution to an operator-installed,
// trusted Runtime Host. The signed plugin manifest selects only DriverName;
// it can never select HostCommand or claim a stronger isolation level.
type IsolationExecutionDriver struct {
	DriverName  string
	Level       IsolationLevel
	HostCommand string
	HostArgs    []string
}

func (d IsolationExecutionDriver) Name() string              { return d.DriverName }
func (d IsolationExecutionDriver) Isolation() IsolationLevel { return d.Level }

func (d IsolationExecutionDriver) Start(ctx context.Context, host *protocolbus.Host, plugin InstalledPlugin,
	policy protocolbus.LaunchPolicy) (*protocolbus.PluginInstance, error) {
	spec, err := d.launchSpec(plugin)
	if err != nil {
		return nil, err
	}
	return host.LaunchSpecWithPolicy(ctx, spec, policy)
}

func (d IsolationExecutionDriver) launchSpec(plugin InstalledPlugin) (protocolbus.LaunchSpec, error) {
	if strings.TrimSpace(d.DriverName) == "" {
		return protocolbus.LaunchSpec{}, errors.New("隔离执行驱动名称不能为空")
	}
	if _, ok := isolationRank[d.Level]; !ok || isolationRank[d.Level] < isolationRank[IsolationProcessSandbox] {
		return protocolbus.LaunchSpec{}, fmt.Errorf("隔离执行驱动 %s 的等级无效: %s", d.DriverName, d.Level)
	}
	command := strings.TrimSpace(d.HostCommand)
	if command == "" {
		return protocolbus.LaunchSpec{}, fmt.Errorf("隔离执行驱动 %s 未配置可信 Runtime Host", d.DriverName)
	}
	if !filepath.IsAbs(command) {
		resolved, err := exec.LookPath(command)
		if err != nil || !filepath.IsAbs(resolved) {
			return protocolbus.LaunchSpec{}, fmt.Errorf("隔离 Runtime Host 必须解析为绝对路径: %s", command)
		}
		command = resolved
	}
	info, err := os.Stat(command)
	if err != nil {
		return protocolbus.LaunchSpec{}, fmt.Errorf("检查隔离 Runtime Host: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return protocolbus.LaunchSpec{}, fmt.Errorf("隔离 Runtime Host 不是可执行普通文件: %s", command)
	}
	root, err := filepath.Abs(plugin.Root)
	if err != nil || strings.TrimSpace(plugin.Root) == "" {
		return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 缺少绝对安装根目录", plugin.ID)
	}
	entry, err := filepath.Abs(plugin.EntryPath)
	if err != nil || strings.TrimSpace(plugin.EntryPath) == "" {
		return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 缺少绝对入口", plugin.ID)
	}
	relative, err := filepath.Rel(root, entry)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return protocolbus.LaunchSpec{}, fmt.Errorf("插件 %s 的入口逃逸安装根目录", plugin.ID)
	}
	args := append(append([]string(nil), d.HostArgs...),
		"--plugin-id", plugin.ID,
		"--plugin-root", root,
		"--entry", entry,
		"--",
	)
	args = append(args, plugin.Execution.Args...)
	return processLaunchSpec(plugin, command, args, d.DriverName), nil
}

func configuredIsolationDrivers() []PluginExecutionDriver {
	configs := []struct {
		name  string
		level IsolationLevel
		env   string
	}{
		{name: "process-sandbox", level: IsolationProcessSandbox, env: processSandboxHostEnv},
		{name: "container", level: IsolationContainer, env: containerHostEnv},
		{name: "wasm-component", level: IsolationWASM, env: wasmComponentHostEnv},
	}
	drivers := make([]PluginExecutionDriver, 0, len(configs))
	for _, config := range configs {
		command := strings.TrimSpace(os.Getenv(config.env))
		if command == "" {
			continue
		}
		drivers = append(drivers, IsolationExecutionDriver{
			DriverName: config.name, Level: config.level, HostCommand: command,
		})
	}
	return drivers
}
