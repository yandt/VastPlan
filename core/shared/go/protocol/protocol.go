// Package protocol 是宿主协议常量的**唯一真源**（ADR-0017 §3）。
//
// 宿主（core/shared/go/protocolbus）与插件 SDK（extensions/sdk/go/plugin）都从这里取，
// 禁止两处各自声明——否则版本协商会因两侧漂移而失效。
package protocol

import (
	"fmt"

	"github.com/Masterminds/semver/v3"
)

// MagicCookie 防止误把普通进程当插件（插件契约与协议 §2.2）。
const MagicCookie = "VASTPLAN_PLUGIN_V1"

const MaxPluginConfigBytes = 64 << 10

// 宿主拉起插件时经环境变量注入的三件套（§2.2「注入连接端点 + magic cookie」）。
const (
	// MagicEnvKey magic cookie。
	MagicEnvKey = "VASTPLAN_PLUGIN_MAGIC"
	// HostAddrEnvKey 宿主的监听地址——插件回连它（宿主是服务端）。
	HostAddrEnvKey = "VASTPLAN_HOST_ADDR"
	// LaunchTokenEnvKey 一次性令牌，用于把握手对应回那次 Launch。
	LaunchTokenEnvKey = "VASTPLAN_LAUNCH_TOKEN"
	// PluginConfigEnvKey carries the caller-isolated, non-sensitive startup
	// snapshot. Managed credential values are never included in this document.
	PluginConfigEnvKey = "VASTPLAN_PLUGIN_CONFIG_JSON"
	// RuntimeAudienceEnvKey is a non-secret digest of the host-verified launch
	// identity. A plugin may compare it with a returned encrypted lease, but it
	// cannot choose or override the value.
	RuntimeAudienceEnvKey = "VASTPLAN_RUNTIME_AUDIENCE"
)

// SessionMetadataKey 插件在 Channel 流的 gRPC metadata 中携带会话票据的键。
// 必须小写：gRPC metadata 键大小写不敏感但规范化为小写。
const SessionMetadataKey = "vastplan-session-id"

// SupportedVersions 本代码库支持的协议版本集。
//
// 协议版本用**单调整数**而非 SemVer（ADR-0017 §3）：它只回答"能不能通话"，
// 握手取交集即可，MINOR/PATCH 语义对它无意义。
var SupportedVersions = []int32{1}

const (
	FeatureDynamicContributions = "contribution.dynamic.v1"
	FeatureCancellation         = "channel.cancel.v1"
	FeatureEventPublish         = "event.publish.v1"
)

// SupportedFeatures 允许在不抬高 wire 主版本的前提下协商可选能力。新增能力只追加。
var SupportedFeatures = []string{
	FeatureCancellation,
	FeatureDynamicContributions,
	FeatureEventPublish,
}

// NegotiateFeatures 返回双方交集并保持宿主声明顺序，便于日志和测试确定化。
func NegotiateFeatures(offered, supported []string) []string {
	want := make(map[string]struct{}, len(offered))
	for _, feature := range offered {
		want[feature] = struct{}{}
	}
	result := make([]string, 0, len(supported))
	for _, feature := range supported {
		if _, ok := want[feature]; ok {
			result = append(result, feature)
		}
	}
	return result
}

func HasFeature(features []string, feature string) bool {
	for _, candidate := range features {
		if candidate == feature {
			return true
		}
	}
	return false
}

// Negotiate 取双方版本集交集里最高的；无交集返回 -1（调用方据此 fail-closed 拒绝）。
func Negotiate(a, b []int32) int32 {
	best := int32(-1)
	for _, x := range a {
		for _, y := range b {
			if x == y && x > best {
				best = x
			}
		}
	}
	return best
}

// Supports 判断某版本是否被本库支持。
func Supports(v int32) bool {
	for _, x := range SupportedVersions {
		if x == v {
			return true
		}
	}
	return false
}

// CheckEngine 校验内核版本是否满足插件 engines 声明的 SemVer 范围（ADR-0017 §4 强制点 2）。
//
// constraint 为空表示插件**未声明**对该内核的兼容性 —— 一律拒绝（fail-closed），
// 因为那说明它本就不该被装进这个内核。
func CheckEngine(kernelName, kernelVersion, constraint string) error {
	if constraint == "" {
		return fmt.Errorf("插件未声明对内核 %q 的 engines 兼容范围（fail-closed 拒绝）", kernelName)
	}
	v, err := semver.NewVersion(kernelVersion)
	if err != nil {
		return fmt.Errorf("内核版本 %q 非法 SemVer: %w", kernelVersion, err)
	}
	c, err := semver.NewConstraint(constraint)
	if err != nil {
		return fmt.Errorf("插件 engines.%s = %q 非法约束: %w", kernelName, constraint, err)
	}
	if !c.Check(v) {
		return fmt.Errorf("内核 %s@%s 不满足插件要求的 %q", kernelName, kernelVersion, constraint)
	}
	return nil
}
