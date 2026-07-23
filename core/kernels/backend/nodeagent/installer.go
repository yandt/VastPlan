package nodeagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifacttrust"
)

var sha256DirectoryPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

const (
	DefaultMaxPackageFiles         = artifacttrust.DefaultMaxPackageFiles
	DefaultMaxPackageFileBytes     = artifacttrust.DefaultMaxPackageFileBytes
	DefaultMaxPackageExpandedBytes = artifacttrust.DefaultMaxPackageExpandedBytes
)

// LocalInstaller 把插件解包到 Root/<sha256>。目录名绑定内容而非版本标签，
// 使升级候选可与当前实例并存，运行时成功切换前不会破坏旧版本。
type LocalInstaller struct {
	Root               string
	MaxFiles           int
	MaxFileBytes       int64
	MaxExpandedBytes   int64
	PythonDependencies PythonDependencyInstaller
}

// Install 只接受内核验证器产生的 VerifiedArtifact，并再次复验摘要后以临时目录
// 原子发布安装结果。零值或由来源绕过验证构造的输入会被拒绝。
func (i LocalInstaller) Install(verified VerifiedArtifact) (InstalledPlugin, error) {
	if !verified.verified {
		return InstalledPlugin{}, errors.New("安装器拒绝未经内核验证的制品")
	}
	artifact, packageBytes := verified.artifact, verified.packageBytes
	if strings.TrimSpace(i.Root) == "" {
		return InstalledPlugin{}, errors.New("安装根目录不能为空")
	}
	digest := sha256.Sum256(packageBytes)
	if actual := hex.EncodeToString(digest[:]); actual != artifact.SHA256 {
		return InstalledPlugin{}, fmt.Errorf("安装制品 SHA-256 不匹配：期望 %s，实际 %s", artifact.SHA256, actual)
	}
	if int64(len(packageBytes)) != artifact.Size {
		return InstalledPlugin{}, fmt.Errorf("安装制品大小不匹配：期望 %d，实际 %d", artifact.Size, len(packageBytes))
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("解析制品清单: %w", err)
	}
	if manifest.ID != artifact.PluginID || manifest.Version != artifact.Version {
		return InstalledPlugin{}, errors.New("制品清单与元数据身份不一致")
	}
	entry, ok := manifest.Entry["backend"]
	if !ok {
		return InstalledPlugin{}, errors.New("service 插件缺少 backend 入口")
	}
	execution := pluginv1.BackendExecutionContract(manifest)

	target := filepath.Join(filepath.Clean(i.Root), artifact.SHA256)
	if installed, err := inspectInstalled(target, artifact, manifest.Publisher, entry, execution); err == nil {
		return installed, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstalledPlugin{}, fmt.Errorf("既有安装目录无效: %w", err)
	}
	if err := os.MkdirAll(filepath.Clean(i.Root), 0o755); err != nil {
		return InstalledPlugin{}, fmt.Errorf("创建安装根目录: %w", err)
	}
	tmp, err := os.MkdirTemp(filepath.Clean(i.Root), ".install-*")
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("创建安装临时目录: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(tmp) // 成功 Rename 后路径已不存在；失败路径保留原始安装错误。
	}()
	if err := extractPackage(tmp, packageBytes, i.packageLimits()); err != nil {
		return InstalledPlugin{}, err
	}
	if err := preparePythonEnvironment(tmp, manifest, i.PythonDependencies); err != nil {
		return InstalledPlugin{}, fmt.Errorf("准备 Python 完整依赖环境: %w", err)
	}
	if _, err := inspectInstalled(tmp, artifact, manifest.Publisher, entry, execution); err != nil {
		return InstalledPlugin{}, fmt.Errorf("校验安装结果: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		if installed, checkErr := inspectInstalled(target, artifact, manifest.Publisher, entry, execution); checkErr == nil {
			return installed, nil // 并发安装同一内容时接受另一方已完成的原子结果。
		}
		return InstalledPlugin{}, fmt.Errorf("发布安装目录: %w", err)
	}
	return inspectInstalled(target, artifact, manifest.Publisher, entry, execution)
}

type packageLimits struct {
	maxFiles         int
	maxFileBytes     int64
	maxExpandedBytes int64
}

func (i LocalInstaller) packageLimits() packageLimits {
	limits := packageLimits{maxFiles: i.MaxFiles, maxFileBytes: i.MaxFileBytes, maxExpandedBytes: i.MaxExpandedBytes}
	if limits.maxFiles <= 0 {
		limits.maxFiles = DefaultMaxPackageFiles
	}
	if limits.maxFileBytes <= 0 {
		limits.maxFileBytes = DefaultMaxPackageFileBytes
	}
	if limits.maxExpandedBytes <= 0 {
		limits.maxExpandedBytes = DefaultMaxPackageExpandedBytes
	}
	return limits
}

// GarbageCollect 删除 Root 下未被实际态引用的内容寻址目录。
// 只识别严格的 64 位 SHA-256 目录，临时文件和运维放入的其他目录一律不碰。
func (i LocalInstaller) GarbageCollect(keepSHA256 []string) error {
	if strings.TrimSpace(i.Root) == "" {
		return errors.New("安装根目录不能为空")
	}
	entries, err := os.ReadDir(filepath.Clean(i.Root))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("读取安装根目录: %w", err)
	}
	keep := make(map[string]struct{}, len(keepSHA256))
	for _, sha := range keepSHA256 {
		keep[sha] = struct{}{}
	}
	for _, entry := range entries {
		if !entry.IsDir() || !sha256DirectoryPattern.MatchString(entry.Name()) {
			continue
		}
		if _, ok := keep[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(filepath.Clean(i.Root), entry.Name())); err != nil {
			return fmt.Errorf("清理未引用安装 %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func extractPackage(root string, packageBytes []byte, limits packageLimits) error {
	gz, err := gzip.NewReader(bytes.NewReader(packageBytes))
	if err != nil {
		return fmt.Errorf("打开插件包: %w", err)
	}
	defer func() {
		_ = gz.Close() // 只读 reader 无待提交数据，解析错误优先返回。
	}()
	tr := tar.NewReader(gz)
	files := 0
	var expandedBytes int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("读取插件包: %w", err)
		}
		name, err := safeArchiveName(h.Name)
		if err != nil {
			return err
		}
		if h.Typeflag != tar.TypeReg && h.Typeflag != 0 { // 0 是旧 tar 规范的普通文件标记。
			return fmt.Errorf("插件包只允许普通文件: %s", name)
		}
		if h.Size < 0 || h.Size > limits.maxFileBytes {
			return fmt.Errorf("插件包文件 %s 大小 %d 超过单文件上限 %d", name, h.Size, limits.maxFileBytes)
		}
		files++
		if files > limits.maxFiles {
			return fmt.Errorf("插件包文件数超过上限 %d", limits.maxFiles)
		}
		expandedBytes += h.Size
		if expandedBytes > limits.maxExpandedBytes {
			return fmt.Errorf("插件包展开大小 %d 超过上限 %d", expandedBytes, limits.maxExpandedBytes)
		}
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(h.Mode) & 0o755
		if mode == 0 {
			mode = 0o644
		}
		f, err := os.OpenFile(filename, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
		if err != nil {
			return fmt.Errorf("创建安装文件 %s: %w", name, err)
		}
		_, copyErr := io.CopyN(f, tr, h.Size)
		closeErr := f.Close()
		if copyErr != nil {
			return fmt.Errorf("写入安装文件 %s: %w", name, copyErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
}

func safeArchiveName(name string) (string, error) {
	if name == "" || path.IsAbs(name) {
		return "", fmt.Errorf("非法插件包路径 %q", name)
	}
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("插件包路径逃逸 %q", name)
	}
	return clean, nil
}

func inspectInstalled(root string, artifact pluginv1.Artifact, publisher, entry string, execution pluginv1.BackendExecution) (InstalledPlugin, error) {
	manifestRaw, err := os.ReadFile(filepath.Join(root, "vastplan.plugin.json"))
	if err != nil {
		return InstalledPlugin{}, err
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		return InstalledPlugin{}, err
	}
	if manifest.ID != artifact.PluginID || manifest.Version != artifact.Version {
		return InstalledPlugin{}, errors.New("安装清单身份不一致")
	}
	contributions, err := pluginv1.BackendRuntimeContributions(manifest)
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("冻结运行时贡献: %w", err)
	}
	entryPath := filepath.Join(root, filepath.FromSlash(entry))
	info, err := os.Stat(entryPath)
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("backend 入口不存在: %w", err)
	}
	if !info.Mode().IsRegular() {
		return InstalledPlugin{}, fmt.Errorf("backend 入口不是普通文件: %s", entry)
	}
	if execution.Driver == "native" && info.Mode().Perm()&0o111 == 0 {
		return InstalledPlugin{}, fmt.Errorf("native backend 入口不可执行: %s", entry)
	}
	var dynamicGoPath string
	if execution.DynamicGo != nil {
		if execution.DynamicGo.ABI != "vastplan.dynamic-go.v1" {
			return InstalledPlugin{}, fmt.Errorf("dynamic-go ABI 不受支持: %s", execution.DynamicGo.ABI)
		}
		if !sha256DirectoryPattern.MatchString(execution.DynamicGo.Fingerprint) {
			return InstalledPlugin{}, errors.New("dynamic-go 制品缺少构建时注入的 SHA-256 指纹")
		}
		dynamicGoPath = filepath.Join(root, filepath.FromSlash(execution.DynamicGo.Entry))
		dynamicInfo, err := os.Stat(dynamicGoPath)
		if err != nil {
			return InstalledPlugin{}, fmt.Errorf("dynamic-go 入口不存在: %w", err)
		}
		if !dynamicInfo.Mode().IsRegular() {
			return InstalledPlugin{}, fmt.Errorf("dynamic-go 入口不是普通文件: %s", execution.DynamicGo.Entry)
		}
	}
	pythonPath, err := inspectPythonEnvironment(root, manifest)
	if err != nil {
		return InstalledPlugin{}, fmt.Errorf("校验 Python 完整依赖环境: %w", err)
	}
	installed := InstalledPlugin{
		ID: artifact.PluginID, Publisher: publisher, Version: artifact.Version, Channel: artifact.Channel,
		Engines: cloneStringMap(manifest.Engines),
		SHA256:  artifact.SHA256, Root: root, EntryPath: entryPath, DynamicGoPath: dynamicGoPath, PythonPath: pythonPath,
		Execution: execution,
		Contract:  PluginRuntimeContract{Contributions: contributions, ContextAccess: pluginv1.ContextAccessContract(manifest)},
	}
	if manifest.Runtime != nil {
		installed.Contract.Requires = append([]pluginv1.RuntimeRequirement(nil), manifest.Runtime.Requires...)
	}
	if manifest.Capabilities != nil {
		installed.Contract.KernelServices = append([]string(nil), manifest.Capabilities.KernelServices...)
	}
	if manifest.State != nil && manifest.State.Backend != nil {
		state := manifest.State.Backend
		installed.State = &PluginStateContract{
			PluginStateIdentity: pluginStateIdentity(state.StateIdentity),
		}
		if state.Migration != nil {
			installed.State.MigrationProtocol = state.Migration.Protocol
			for _, from := range state.Migration.From {
				installed.State.MigrationFrom = append(installed.State.MigrationFrom, pluginStateIdentity(from))
			}
		}
	}
	return installed, nil
}
