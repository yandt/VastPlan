package protocolbus

import (
	"strings"
	"testing"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocol"
)

func TestLaunchPolicyCreatesReservedRuntimeAudience(t *testing.T) {
	policy := LaunchPolicy{
		PluginID: "cn.vastplan.foundation.data.relational.runtime", Publisher: "vastplan", Version: "0.2.0",
		ArtifactSHA256: strings.Repeat("a", 64), NodeID: "node-a", RuntimeScope: "database", RuntimeInstanceID: "runtime-a",
	}
	environment, err := runtimeAudienceEnvironment(policy)
	if err != nil || !strings.HasPrefix(environment, protocol.RuntimeAudienceEnvKey+"=runtime:v1:") {
		t.Fatalf("runtime audience 环境无效: %q err=%v", environment, err)
	}
	if err := validateExtraEnvironment([]string{environment}); err == nil {
		t.Fatal("执行驱动不得覆盖宿主保留的 runtime audience")
	}
	for _, inherited := range pluginEnvironment([]string{protocol.RuntimeAudienceEnvKey}) {
		if strings.HasPrefix(inherited, protocol.RuntimeAudienceEnvKey+"=") {
			t.Fatal("runtime audience 不得从父进程继承")
		}
	}
}
