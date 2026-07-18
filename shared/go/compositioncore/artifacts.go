// Package compositioncore contains cross-kernel composition policy that can be
// shared by Go kernels. It does not know about Backend service units, Frontend
// modules, Runner profiles or Mobile bundles.
package compositioncore

import (
	"fmt"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/schemas/composition/common/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/shared/go/pluginid"
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
}

// VerifyRef verifies an exact immutable artifact before deriving management
// class from its signed manifest. The caller owns topology and output-specific
// collision rules; this function owns the cross-kernel trust boundary.
func VerifyRef(ref Selection, origin string, seen map[string]Selection, artifacts ArtifactReader, options Options) error {
	if err := compositioncommonv1.ValidateOrigin(origin); err != nil {
		return err
	}
	ref.Channel = NormalizeChannel(ref.Channel)
	if previous, ok := seen[ref.ID]; ok {
		if previous.Version != ref.Version || NormalizeChannel(previous.Channel) != ref.Channel {
			return fmt.Errorf("插件 %q 存在多版本或多 channel 冲突", ref.ID)
		}
		return nil
	}
	artifact, _, err := artifacts.Read(pluginv1.ArtifactRef{PluginID: ref.ID, Version: ref.Version, Channel: ref.Channel})
	if err != nil {
		return fmt.Errorf("读取制品 %s@%s/%s: %w", ref.ID, ref.Version, ref.Channel, err)
	}
	manifest, err := pluginv1.ParseManifest(artifact.Manifest)
	if err != nil {
		return fmt.Errorf("制品 %s 清单无效: %w", ref.ID, err)
	}
	if artifact.PluginID != ref.ID || artifact.Version != ref.Version || NormalizeChannel(artifact.Channel) != ref.Channel || manifest.ID != ref.ID || manifest.Version != ref.Version {
		return fmt.Errorf("制品引用与不可变清单身份不一致: %s@%s/%s", ref.ID, ref.Version, ref.Channel)
	}
	class, err := pluginid.ClassifyManagement(manifest.ID, manifest.Publisher)
	if err != nil {
		return fmt.Errorf("插件 %s 身份分类失败: %w", ref.ID, err)
	}
	if class == pluginid.ManagementDevelopment && !options.AllowDevelopmentPlugins {
		return fmt.Errorf("开发插件 %q 未被生产策略允许", ref.ID)
	}
	if origin == compositioncommonv1.OriginApplication && class == pluginid.ManagementPlatform {
		return fmt.Errorf("应用配置不能选择平台管理插件 %q", ref.ID)
	}
	seen[ref.ID] = ref
	return nil
}

func NormalizeChannel(channel string) string {
	if channel == "" {
		return "stable"
	}
	return channel
}
