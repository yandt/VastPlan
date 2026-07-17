// Package artifacttrust 定义内核不可委托给制品源的内容验证边界。
// 制品源（包括未来基础插件）只能返回 Envelope；调用方必须在安装前重新验证。
package artifacttrust

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"

	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
)

const (
	manifestName      = "vastplan.plugin.json"
	maxManifestBytes  = 1 << 20
	maxLegalFileBytes = 1 << 20

	// 安全上限由信任验证与安装器共同使用，避免验证阶段先被压缩炸弹耗尽资源，
	// 或两层使用不同阈值导致“已验证但无法安装”。
	DefaultMaxPackageFiles         = 10_000
	DefaultMaxPackageFileBytes     = int64(256 << 20)
	DefaultMaxPackageExpandedBytes = int64(1 << 30)
)

// ErrNotFound 是允许内核尝试下一个种子/远端制品源的唯一错误。
// 传输错误、格式错误和验证错误不得伪装为 not found 后静默回退。
var ErrNotFound = errors.New("制品不存在")

// Envelope 是制品源返回的未信任载荷。Proof 是可选的发布者证明原始 JSON；
// 即使来源声称已经验证，内核仍必须独立验证 Artifact、PackageBytes 和 Proof。
type Envelope struct {
	Artifact     pluginv1.Artifact
	PackageBytes []byte
	Proof        json.RawMessage
}

// ValidateContent 验证元数据、摘要、大小、tar 根清单和法律文件绑定。
// 它不验证发布者身份；证明验证由内核持有的信任策略完成。
func ValidateContent(artifact pluginv1.Artifact, packageBytes []byte) error {
	metadata, err := json.Marshal(artifact)
	if err != nil {
		return fmt.Errorf("序列化制品元数据: %w", err)
	}
	if err := pluginv1.ValidateArtifactMetadata(metadata); err != nil {
		return err
	}
	digest := sha256.Sum256(packageBytes)
	if actual := hex.EncodeToString(digest[:]); actual != artifact.SHA256 {
		return fmt.Errorf("制品 SHA-256 不匹配：期望 %s，实际 %s", artifact.SHA256, actual)
	}
	if int64(len(packageBytes)) != artifact.Size {
		return fmt.Errorf("制品大小不匹配：期望 %d，实际 %d", artifact.Size, len(packageBytes))
	}
	manifest, manifestRaw, err := InspectPackage(packageBytes)
	if err != nil {
		return fmt.Errorf("制品包内容无效: %w", err)
	}
	if manifest.ID != artifact.PluginID || manifest.Version != artifact.Version || !sameJSON(manifestRaw, artifact.Manifest) {
		return errors.New("制品清单与索引绑定不一致")
	}
	return nil
}

// InspectPackage 只读取 archive metadata 和根清单，绝不执行包内内容。
func InspectPackage(packageBytes []byte) (pluginv1.Manifest, json.RawMessage, error) {
	gz, err := gzip.NewReader(bytes.NewReader(packageBytes))
	if err != nil {
		return pluginv1.Manifest{}, nil, fmt.Errorf("插件包不是 gzip tar: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	var manifestRaw []byte
	entrySizes := map[string]int64{}
	files := 0
	var expandedBytes int64
	for {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return pluginv1.Manifest{}, nil, fmt.Errorf("读取 tar 条目: %w", err)
		}
		name, err := archiveName(header.Name)
		if err != nil {
			return pluginv1.Manifest{}, nil, err
		}
		if header.Typeflag != tar.TypeReg && header.Typeflag != 0 {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件包只允许普通文件: %s", name)
		}
		if header.Size < 0 || header.Size > DefaultMaxPackageFileBytes {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件包文件 %s 大小 %d 超过单文件上限 %d", name, header.Size, DefaultMaxPackageFileBytes)
		}
		if _, exists := entrySizes[name]; exists {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件包包含重复路径: %s", name)
		}
		files++
		if files > DefaultMaxPackageFiles {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件包文件数超过上限 %d", DefaultMaxPackageFiles)
		}
		if header.Size > DefaultMaxPackageExpandedBytes-expandedBytes {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件包展开大小超过上限 %d", DefaultMaxPackageExpandedBytes)
		}
		expandedBytes += header.Size
		entrySizes[name] = header.Size
		if name != manifestName {
			continue
		}
		if manifestRaw != nil {
			return pluginv1.Manifest{}, nil, errors.New("插件包包含多个根清单")
		}
		manifestRaw, err = io.ReadAll(io.LimitReader(tr, maxManifestBytes+1))
		if err != nil {
			return pluginv1.Manifest{}, nil, fmt.Errorf("读取插件清单: %w", err)
		}
		if len(manifestRaw) > maxManifestBytes {
			return pluginv1.Manifest{}, nil, fmt.Errorf("插件清单超过 %d 字节上限", maxManifestBytes)
		}
	}
	if manifestRaw == nil {
		return pluginv1.Manifest{}, nil, errors.New("插件包缺少根清单 vastplan.plugin.json")
	}
	manifest, err := pluginv1.ParseManifest(manifestRaw)
	if err != nil {
		return pluginv1.Manifest{}, nil, err
	}
	if manifest.License != "" {
		if err := validatePackagedLegalFile(entrySizes, manifest.LicenseFile, "许可证"); err != nil {
			return pluginv1.Manifest{}, nil, err
		}
		if manifest.NoticeFile != "" {
			if err := validatePackagedLegalFile(entrySizes, manifest.NoticeFile, "归属告示"); err != nil {
				return pluginv1.Manifest{}, nil, err
			}
		}
	}
	return manifest, json.RawMessage(manifestRaw), nil
}

func validatePackagedLegalFile(entrySizes map[string]int64, declaredName, kind string) error {
	name, err := archiveName(declaredName)
	if err != nil {
		return fmt.Errorf("非法%s文件路径: %w", kind, err)
	}
	size, exists := entrySizes[name]
	if !exists {
		return fmt.Errorf("插件包必须包含且仅包含一份%s文件 %s", kind, name)
	}
	if size <= 0 || size > maxLegalFileBytes {
		return fmt.Errorf("%s文件 %s 大小必须在 1..%d 字节内", kind, name, maxLegalFileBytes)
	}
	return nil
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

func sameJSON(left, right []byte) bool {
	var a, b bytes.Buffer
	if json.Compact(&a, left) != nil || json.Compact(&b, right) != nil {
		return false
	}
	return bytes.Equal(a.Bytes(), b.Bytes())
}
