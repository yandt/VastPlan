// Package pluginservice 实现后端内核内置的插件服务最小制品能力。
//
// 它只负责制品的验证、不可变存储和校验读取；部署执行属于 Node Agent reconcile，
// 不可把拉起进程、节点选择或 NATS 控制面混进这里（ADR-0010）。
package pluginservice

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/artifacttrust"
)

const (
	manifestName      = "vastplan.plugin.json"
	artifactExtension = ".tar.gz"
	maxLegalFileBytes = 1 << 20 // 法律声明是文本；限制大小避免被用作绕过制品检查的大对象。
)

var channelPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Ref 与 Artifact 保留为兼容别名；稳定 DTO 的单一真源在 schemas/plugin/v1。
type Ref = pluginv1.ArtifactRef
type Artifact = pluginv1.Artifact

// Repository 是本地制品仓库。Root 由插件服务配置；其磁盘布局是实现细节，
// 调用方必须通过 Publish/Read 而非拼接路径访问，才能保留 SHA-256 fail-closed 语义。
type Repository struct {
	root string
	mu   sync.Mutex // 单进程发布串行化，保证索引检查与不可变写入不可交错。
}

func (r *Repository) SourceName() string { return "local-file" }

// NewRepository 创建本地仓库句柄。目录延迟到首次发布时才创建，方便只读调用方
// 在尚未初始化的机器上得到清晰的 not found 错误。
func NewRepository(root string) (*Repository, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("制品仓库根目录不能为空")
	}
	return &Repository{root: filepath.Clean(root)}, nil
}

// PackageDirectory 将一个插件目录打成 gzip tar 包。它首先校验根目录的清单，
// 且拒绝符号链接和目录逃逸路径，避免制品在未来被 Node Agent 解包时突破安装目录。
func PackageDirectory(dir string) ([]byte, pluginv1.Manifest, error) {
	manifestPath := filepath.Join(dir, manifestName)
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, pluginv1.Manifest{}, fmt.Errorf("读取插件清单: %w", err)
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		return nil, pluginv1.Manifest{}, err
	}
	if err := validateLegalFiles(dir, manifest); err != nil {
		return nil, pluginv1.Manifest{}, err
	}

	var out bytes.Buffer
	gz := gzip.NewWriter(&out)
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(dir, func(filename string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if filename == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, filename)
		if err != nil {
			return err
		}
		name, err := archiveName(rel)
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("插件包不允许符号链接: %s", name)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("插件包只允许普通文件: %s", name)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = name
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		f, err := os.Open(filename)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return nil, pluginv1.Manifest{}, fmt.Errorf("打包插件目录: %w", err)
	}
	if err := tw.Close(); err != nil {
		_ = gz.Close()
		return nil, pluginv1.Manifest{}, fmt.Errorf("完成 tar 包: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, pluginv1.Manifest{}, fmt.Errorf("完成 gzip 包: %w", err)
	}
	return out.Bytes(), manifest, nil
}

// Publish 校验并不可变地保存一个插件包。相同 ref 仅允许幂等重传完全相同的字节；
// 任何不同 SHA 都会被拒绝，防止版本标签被悄悄改指向另一份代码。
func (r *Repository) Publish(channel string, packageBytes []byte) (Artifact, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	artifact, err := Describe(channel, packageBytes)
	if err != nil {
		return Artifact{}, err
	}
	return r.publishArtifact(artifact, packageBytes)
}

// Describe 只根据待发布字节生成确定性的制品元数据，不产生存储副作用。
// 供应链签名必须先得到这份精确元数据再签名，不能签一个会被服务端重新解释的版本标签。
func Describe(channel string, packageBytes []byte) (Artifact, error) {
	if !channelPattern.MatchString(channel) {
		return Artifact{}, fmt.Errorf("非法发布 channel %q", channel)
	}
	if len(packageBytes) == 0 {
		return Artifact{}, errors.New("插件包不能为空")
	}
	manifest, manifestRaw, err := inspectPackage(packageBytes)
	if err != nil {
		return Artifact{}, err
	}

	digest := sha256.Sum256(packageBytes)
	sha := hex.EncodeToString(digest[:])
	artifact := Artifact{
		SchemaVersion: "v1",
		PluginID:      manifest.ID,
		Version:       manifest.Version,
		Channel:       channel,
		SHA256:        sha,
		Size:          int64(len(packageBytes)),
		Object:        sha + artifactExtension,
		Manifest:      manifestRaw,
	}
	metadata, err := json.Marshal(artifact)
	if err != nil {
		return Artifact{}, fmt.Errorf("序列化制品元数据: %w", err)
	}
	if err := pluginv1.ValidateArtifactMetadata(metadata); err != nil {
		return Artifact{}, err
	}

	return artifact, nil
}

func (r *Repository) publishArtifact(artifact Artifact, packageBytes []byte) (Artifact, error) {
	if err := ValidateArtifact(artifact, packageBytes); err != nil {
		return Artifact{}, err
	}
	metadata, err := json.Marshal(artifact)
	if err != nil {
		return Artifact{}, fmt.Errorf("序列化制品元数据: %w", err)
	}
	dir, err := r.artifactDir(Ref{PluginID: artifact.PluginID, Version: artifact.Version, Channel: artifact.Channel})
	if err != nil {
		return Artifact{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Artifact{}, fmt.Errorf("创建制品目录: %w", err)
	}
	indexPath := filepath.Join(dir, "artifact.json")
	if existing, err := readArtifact(indexPath); err == nil {
		if existing.SHA256 != artifact.SHA256 {
			return Artifact{}, fmt.Errorf("制品 %s@%s/%s 已存在且 SHA-256 不同，版本不可变",
				artifact.PluginID, artifact.Version, artifact.Channel)
		}
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Artifact{}, fmt.Errorf("读取既有制品索引: %w", err)
	}

	if err := writeFileAtomically(filepath.Join(dir, artifact.Object), packageBytes, 0o644); err != nil {
		return Artifact{}, fmt.Errorf("写入制品对象: %w", err)
	}
	if err := writeFileAtomically(indexPath, append(metadata, '\n'), 0o644); err != nil {
		return Artifact{}, fmt.Errorf("写入制品索引: %w", err)
	}
	return artifact, nil
}

// Read 读取一个制品并在交付前复算 SHA-256、重新检查 tar 内清单和索引绑定。
// 因而磁盘损坏、对象被替换或索引被手工改错都不会被下游节点当作可安装制品。
func (r *Repository) Read(ref Ref) (Artifact, []byte, error) {
	dir, err := r.artifactDir(ref)
	if err != nil {
		return Artifact{}, nil, err
	}
	artifact, err := readArtifact(filepath.Join(dir, "artifact.json"))
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("读取制品索引: %w", err)
	}
	if artifact.PluginID != ref.PluginID || artifact.Version != ref.Version || artifact.Channel != ref.Channel {
		return Artifact{}, nil, errors.New("制品索引与请求引用不一致")
	}
	packagePath := filepath.Join(dir, artifact.Object)
	packageBytes, err := os.ReadFile(packagePath)
	if err != nil {
		return Artifact{}, nil, fmt.Errorf("读取制品对象: %w", err)
	}
	if err := ValidateArtifact(artifact, packageBytes); err != nil {
		return Artifact{}, nil, err
	}
	return artifact, packageBytes, nil
}

// Fetch 实现内核 ArtifactSource。返回值仍被视为未信任 Envelope；本地仓库的
// Read 校验只是纵深防御，不能替代 Node Agent 的统一验证强制点。
func (r *Repository) Fetch(_ context.Context, ref Ref) (artifacttrust.Envelope, error) {
	artifact, packageBytes, err := r.Read(ref)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return artifacttrust.Envelope{}, fmt.Errorf("%w: %s@%s/%s", artifacttrust.ErrNotFound, ref.PluginID, ref.Version, ref.Channel)
		}
		return artifacttrust.Envelope{}, err
	}
	return artifacttrust.Envelope{Artifact: artifact, PackageBytes: packageBytes}, nil
}

// ValidateArtifact 对任意来源的制品执行与本地仓库 Read 相同的 fail-closed 校验。
// 远端客户端下载后必须调用它，不能因为 HTTPS 或签名正确就跳过内容和清单绑定检查。
func ValidateArtifact(artifact Artifact, packageBytes []byte) error {
	return artifacttrust.ValidateContent(artifact, packageBytes)
}

func (r *Repository) artifactDir(ref Ref) (string, error) {
	if _, err := pluginv1.ParseManifest(minimalManifest(ref.PluginID, ref.Version)); err != nil {
		return "", fmt.Errorf("非法制品引用: %w", err)
	}
	if !channelPattern.MatchString(ref.Channel) {
		return "", fmt.Errorf("非法发布 channel %q", ref.Channel)
	}
	return filepath.Join(r.root, "artifacts", ref.PluginID, ref.Version, ref.Channel), nil
}

func minimalManifest(id, version string) []byte {
	return []byte(fmt.Sprintf(`{"id":%q,"name":"reference","description":"reference","version":%q,"publisher":"reference","engines":{"backend":"^0.0"},"activation":["onStartup"],"entry":{"backend":"backend/main"},"contributes":{"backend":{"tools":[]}}}`, id, version))
}

func readArtifact(filename string) (Artifact, error) {
	raw, err := os.ReadFile(filename)
	if err != nil {
		return Artifact{}, err
	}
	if err := pluginv1.ValidateArtifactMetadata(raw); err != nil {
		return Artifact{}, err
	}
	var artifact Artifact
	if err := json.Unmarshal(raw, &artifact); err != nil {
		return Artifact{}, fmt.Errorf("解析制品索引: %w", err)
	}
	return artifact, nil
}

func writeFileAtomically(filename string, data []byte, mode os.FileMode) error {
	f, err := os.CreateTemp(filepath.Dir(filename), ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, filename)
}

// sameJSON 以规范化 JSON 文本比较。索引经 json.Marshal 后会压缩 RawMessage 的空白，
// 但空白差异不是清单内容变更；结构或值一旦变化则仍会被拒绝。
func sameJSON(left, right []byte) bool {
	var a, b bytes.Buffer
	if json.Compact(&a, left) != nil || json.Compact(&b, right) != nil {
		return false
	}
	return bytes.Equal(a.Bytes(), b.Bytes())
}

// validateLegalFiles 在打包前确认清单声明的许可证与归属文本确实位于插件目录内。
// 旧 v1 清单可不声明许可证；一旦声明则必须随制品提供完整文本（ADR-0046）。
func validateLegalFiles(dir string, manifest pluginv1.Manifest) error {
	if manifest.License == "" {
		return nil
	}
	if err := validateLegalFile(dir, manifest.LicenseFile, "许可证"); err != nil {
		return err
	}
	if manifest.NoticeFile != "" {
		return validateLegalFile(dir, manifest.NoticeFile, "归属告示")
	}
	return nil
}

func validateLegalFile(dir, declaredName, kind string) error {
	name, err := archiveName(declaredName)
	if err != nil {
		return fmt.Errorf("非法%s文件路径: %w", kind, err)
	}
	info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		return fmt.Errorf("读取%s文件 %s: %w", kind, name, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("%s文件必须是普通文件: %s", kind, name)
	}
	if info.Size() == 0 || info.Size() > maxLegalFileBytes {
		return fmt.Errorf("%s文件 %s 大小必须在 1..%d 字节内", kind, name, maxLegalFileBytes)
	}
	return nil
}

// inspectPackage 只读取 archive metadata 和根清单，绝不执行包内内容。
func inspectPackage(packageBytes []byte) (pluginv1.Manifest, json.RawMessage, error) {
	return artifacttrust.InspectPackage(packageBytes)
}

func archiveName(name string) (string, error) {
	if name == "" || path.IsAbs(name) {
		return "", fmt.Errorf("非法插件包路径 %q", name)
	}
	clean := path.Clean(strings.ReplaceAll(name, "\\", "/"))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("插件包路径逃逸 %q", name)
	}
	return clean, nil
}
