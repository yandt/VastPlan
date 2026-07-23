package nodeagent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/pythonlock"
)

const pythonEnvironmentSchemaVersion = "v1"

type PythonDependencyInstaller interface {
	Name() string
	Materialize(context.Context, string, pythonlock.Summary) error
}

// PipPythonDependencyInstaller is an offline adapter, not a resolver. The
// signed pylock and verified local wheel directory are its only inputs.
type PipPythonDependencyInstaller struct {
	Interpreter string
	Timeout     time.Duration
}

func (PipPythonDependencyInstaller) Name() string { return "pip-offline" }

func (i PipPythonDependencyInstaller) Materialize(parent context.Context, root string, summary pythonlock.Summary) error {
	sitePackages := pythonSitePackages(root)
	if err := os.MkdirAll(sitePackages, 0o755); err != nil {
		return err
	}
	interpreter := strings.TrimSpace(i.Interpreter)
	if interpreter == "" {
		interpreter = "python3"
	}
	timeout := i.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	if err := verifyPythonVersion(ctx, interpreter, summary.RequiresPython); err != nil {
		return err
	}
	if len(summary.Packages) == 0 {
		return nil
	}
	requirements := make([]string, 0, len(summary.Packages))
	for _, item := range summary.Packages {
		line := item.Name + "==" + item.Version
		if item.Marker != "" {
			line += " ; " + item.Marker
		}
		requirements = append(requirements, line)
	}
	sort.Strings(requirements)
	requirementsFile := filepath.Join(pythonEnvironmentRoot(root), "requirements.lock.txt")
	if err := os.WriteFile(requirementsFile, []byte(strings.Join(requirements, "\n")+"\n"), 0o600); err != nil {
		return err
	}
	wheelDirectory := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(pythonlock.WheelPathPrefix, "/")))
	args := []string{
		"-m", "pip", "install", "--disable-pip-version-check", "--no-input", "--no-index", "--no-deps",
		"--only-binary=:all:", "--no-compile", "--no-cache-dir", "--find-links", wheelDirectory,
		"--target", sitePackages, "--requirement", requirementsFile,
	}
	command := exec.CommandContext(ctx, interpreter, args...)
	command.Dir = root
	command.Env = offlinePythonEnvironment()
	output := &boundedOutput{limit: 64 << 10}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("离线安装 Python wheel 超时")
		}
		return fmt.Errorf("离线安装 Python wheel: %w: %s", err, output.String())
	}
	return nil
}

func verifyPythonVersion(ctx context.Context, interpreter, requirement string) error {
	if strings.TrimSpace(requirement) == "" {
		return errors.New("Python 依赖锁缺少 requires-python")
	}
	const script = `import sys
from pip._vendor.packaging.specifiers import SpecifierSet
from pip._vendor.packaging.version import Version
version = Version(".".join(str(part) for part in sys.version_info[:3]))
raise SystemExit(0 if version in SpecifierSet(sys.argv[1]) else 78)
`
	command := exec.CommandContext(ctx, interpreter, "-c", script, requirement)
	command.Env = offlinePythonEnvironment()
	output := &boundedOutput{limit: 16 << 10}
	command.Stdout, command.Stderr = output, output
	if err := command.Run(); err != nil {
		return fmt.Errorf("Python 解释器不满足锁定范围 %s 或缺少 pip packaging 校验器: %w: %s", requirement, err, output.String())
	}
	return nil
}

type pythonEnvironmentState struct {
	SchemaVersion string `json:"schemaVersion"`
	LockSHA256    string `json:"lockSHA256"`
	Packages      int    `json:"packages"`
	Installer     string `json:"installer"`
}

func preparePythonEnvironment(root string, manifest pluginv1.Manifest, installer PythonDependencyInstaller) error {
	execution := pluginv1.BackendExecutionContract(manifest)
	if execution.Driver != "python" && execution.Driver != "python-subinterpreter" {
		return nil
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.PythonLock == nil {
		return errors.New("已安装 Python 插件缺少 pythonLock 声明")
	}
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(pythonlock.PackagePath)))
	if err != nil {
		return err
	}
	summary, err := pythonlock.Inspect(raw)
	if err != nil {
		return err
	}
	if summary.SHA256 != manifest.SupplyChain.PythonLock.SHA256 {
		return errors.New("安装目录 pylock.toml 摘要与清单不一致")
	}
	if err := verifyExtractedWheels(root, summary); err != nil {
		return err
	}
	if installer == nil {
		installer = PipPythonDependencyInstaller{Interpreter: strings.TrimSpace(os.Getenv("VASTPLAN_PYTHON_INTERPRETER"))}
	}
	if strings.TrimSpace(installer.Name()) == "" {
		return errors.New("Python 依赖安装器名称不能为空")
	}
	if err := os.MkdirAll(pythonEnvironmentRoot(root), 0o755); err != nil {
		return err
	}
	if err := installer.Materialize(context.Background(), root, summary); err != nil {
		return err
	}
	state := pythonEnvironmentState{SchemaVersion: pythonEnvironmentSchemaVersion, LockSHA256: summary.SHA256, Packages: len(summary.Packages), Installer: installer.Name()}
	stateRaw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(pythonEnvironmentStatePath(root), append(stateRaw, '\n'), 0o600)
}

func inspectPythonEnvironment(root string, manifest pluginv1.Manifest) (string, error) {
	execution := pluginv1.BackendExecutionContract(manifest)
	if execution.Driver != "python" && execution.Driver != "python-subinterpreter" {
		return "", nil
	}
	if manifest.SupplyChain == nil || manifest.SupplyChain.PythonLock == nil {
		return "", errors.New("Python 安装缺少完整依赖锁声明")
	}
	raw, err := os.ReadFile(pythonEnvironmentStatePath(root))
	if err != nil {
		return "", err
	}
	var state pythonEnvironmentState
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return "", fmt.Errorf("解析 Python 依赖环境状态: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return "", errors.New("Python 依赖环境状态只能包含一个 JSON 值")
	}
	if state.SchemaVersion != pythonEnvironmentSchemaVersion || state.LockSHA256 != manifest.SupplyChain.PythonLock.SHA256 || state.Packages < 0 || strings.TrimSpace(state.Installer) == "" {
		return "", errors.New("Python 依赖环境状态与签名锁不一致")
	}
	path := pythonSitePackages(root)
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return "", errors.New("Python 依赖 site-packages 不存在")
	}
	return path, nil
}

func verifyExtractedWheels(root string, summary pythonlock.Summary) error {
	for _, wheel := range summary.Wheels {
		filename := filepath.Join(root, filepath.FromSlash(wheel.PackagePath))
		raw, err := os.ReadFile(filename)
		if err != nil {
			return fmt.Errorf("读取已解包 Python wheel %s: %w", wheel.PackagePath, err)
		}
		if int64(len(raw)) != wheel.Size {
			return fmt.Errorf("已解包 Python wheel 大小不一致: %s", wheel.PackagePath)
		}
		digest := sha256.Sum256(raw)
		if hex.EncodeToString(digest[:]) != wheel.SHA256 {
			return fmt.Errorf("已解包 Python wheel 摘要不一致: %s", wheel.PackagePath)
		}
	}
	return nil
}

func pythonEnvironmentRoot(root string) string {
	return filepath.Join(root, ".vastplan", "python")
}

func pythonSitePackages(root string) string {
	return filepath.Join(pythonEnvironmentRoot(root), "site-packages")
}

func pythonEnvironmentStatePath(root string) string {
	return filepath.Join(pythonEnvironmentRoot(root), "environment.json")
}

func offlinePythonEnvironment() []string {
	result := make([]string, 0, len(os.Environ())+7)
	for _, value := range os.Environ() {
		name := value
		if index := strings.IndexByte(value, '='); index >= 0 {
			name = value[:index]
		}
		upper := strings.ToUpper(name)
		if strings.HasPrefix(upper, "PIP_") || upper == "PYTHONPATH" || upper == "PYTHONHOME" {
			continue
		}
		result = append(result, value)
	}
	return append(result,
		"PIP_CONFIG_FILE="+os.DevNull,
		"PIP_NO_INDEX=1",
		"PIP_DISABLE_PIP_VERSION_CHECK=1",
		"PIP_NO_INPUT=1",
		"PYTHONNOUSERSITE=1",
		"PYTHONDONTWRITEBYTECODE=1",
	)
}

type boundedOutput struct {
	buffer bytes.Buffer
	limit  int
}

func (w *boundedOutput) Write(value []byte) (int, error) {
	original := len(value)
	remaining := w.limit - w.buffer.Len()
	if remaining > 0 {
		if len(value) > remaining {
			value = value[:remaining]
		}
		_, _ = w.buffer.Write(value)
	}
	return original, nil
}

func (w *boundedOutput) String() string { return strings.TrimSpace(w.buffer.String()) }
