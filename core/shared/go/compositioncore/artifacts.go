// Package compositioncore contains cross-kernel composition policy that can be
// shared by Go kernels. It does not know about Backend service units, Frontend
// modules, Desktop profiles or Mobile bundles.
package compositioncore

import (
	"crypto/sha256"
	"fmt"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginid"
)

type ArtifactReader interface {
	Read(pluginv1.ArtifactRef) (pluginv1.Artifact, []byte, error)
}

type Options struct {
	AllowDevelopmentPlugins bool
}

// Selection is the minimal, wire-format-independent input needed by the shared
// trust policy. Kernel adapters explicitly convert their own PluginRef DTOs.
type Selection struct {
	ID      string
	Version string
	Channel string
	SHA256  string
}

// ResolvedArtifact is the immutable artifact view used by one composition
// transaction. Topology rules consume its already validated Manifest instead
// of observing the source again.
type ResolvedArtifact struct {
	Selection Selection
	Artifact  pluginv1.Artifact
	Manifest  pluginv1.Manifest
}

// ResolveRef verifies an immutable artifact before deriving management class
// from its signed manifest. A supplied SHA256 is an existing lock and must
// match; an empty SHA256 explicitly asks this resolution stage to create the
// lock. Repeated plugin IDs reuse the first validated view.
func ResolveRef(ref Selection, origin string, seen map[string]ResolvedArtifact, artifacts ArtifactReader, options Options) (ResolvedArtifact, error) {
	if err := compositioncommonv1.ValidateOrigin(origin); err != nil {
		return ResolvedArtifact{}, err
	}
	if seen == nil {
		return ResolvedArtifact{}, fmt.Errorf("插件 %q 缺少解析事务视图", ref.ID)
	}
	ref.Channel = NormalizeChannel(ref.Channel)
	if previous, ok := seen[ref.ID]; ok {
		if previous.Selection.Version != ref.Version || NormalizeChannel(previous.Selection.Channel) != ref.Channel {
			return ResolvedArtifact{}, fmt.Errorf("插件 %q 存在多版本或多 channel 冲突", ref.ID)
		}
		if ref.SHA256 != "" && previous.Selection.SHA256 != ref.SHA256 {
			return ResolvedArtifact{}, fmt.Errorf("插件 %q 的精确 SHA-256 冲突", ref.ID)
		}
		return previous, nil
	}
	if artifacts == nil {
		return ResolvedArtifact{}, fmt.Errorf("插件 %q 缺少制品读取器", ref.ID)
	}
	artifact, _, err := artifacts.Read(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel})
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("读取制品 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("制品 %s 清单无效: %w", ref.ID, err)
	}
	if artifact.PluginID != ref.ID || artifact.Version != ref.Version || NormalizeChannel(artifact.Channel) != ref.Channel || manifest.ID != ref.ID || manifest.Version != ref.Version {
		return ResolvedArtifact{}, fmt.Errorf("制品引用与不可变清单身份不一致: %s@%s/%s", ref.ID, ref.Version, ref.Channel)
	}
	if len(artifact.SHA256) != sha256.Size*2 {
		return ResolvedArtifact{}, fmt.Errorf("制品 %s@%s/%s 缺少精确 SHA-256", ref.ID, ref.Version, ref.Channel)
	}
	if ref.SHA256 != "" && artifact.SHA256 != ref.SHA256 {
		return ResolvedArtifact{}, fmt.Errorf("制品 %s@%s/%s 的摘要 %s 与解析锁 %s 不一致", ref.ID, ref.Version, ref.Channel, artifact.SHA256, ref.SHA256)
	}
	class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
	if err != nil {
		return ResolvedArtifact{}, fmt.Errorf("插件 %s 身份分类失败: %w", ref.ID, err)
	}
	if class == pluginid.ManagementDevelopment && !options.AllowDevelopmentPlugins {
		return ResolvedArtifact{}, fmt.Errorf("开发插件 %q 未被生产策略允许", ref.ID)
	}
	if origin == compositioncommonv1.OriginApplication && class == pluginid.ManagementPlatform {
		return ResolvedArtifact{}, fmt.Errorf("应用配置不能选择平台管理插件 %q", ref.ID)
	}
	ref.SHA256 = artifact.SHA256
	resolved := ResolvedArtifact{Selection: ref, Artifact: artifact, Manifest: manifest}
	seen[ref.ID] = resolved
	return resolved, nil
}

func NormalizeChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
}
