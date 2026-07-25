// composition-service 是 P5 Application Intent E2E 的真实 Go 进程夹具。
// 同一源码按签名 Manifest 的 entry 文件名选择精确插件身份，减少重复代码，
// 但每个打包制品仍拥有独立清单、版本、摘要和发布者证明。
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type identity struct {
	id         string
	version    string
	capability string
}

var identities = map[string]identity{
	"pipeline-root-1.0.0":       {"cn.vastplan.fixture.composition.root", "1.0.0", "fixture.composition.root"},
	"pipeline-audit-1.1.0":      {"cn.vastplan.fixture.composition.audit", "1.1.0", "fixture.composition.audit"},
	"pipeline-audit-1.2.0":      {"cn.vastplan.fixture.composition.audit", "1.2.0", "fixture.composition.audit"},
	"pipeline-quota-1.1.0":      {"cn.vastplan.fixture.composition.quota", "1.1.0", "fixture.composition.quota"},
	"pipeline-common-1.5.0":     {"cn.vastplan.fixture.composition.common", "1.5.0", "fixture.composition.common"},
	"pipeline-common-2.1.0":     {"cn.vastplan.fixture.composition.common", "2.1.0", "fixture.composition.common"},
	"pipeline-common-workspace": {"cn.vastplan.fixture.composition.common", "1.5.0", "fixture.composition.common"},
	"pipeline-conflict-1.0.0":   {"cn.vastplan.fixture.composition.conflict", "1.0.0", "fixture.composition.conflict"},
	"pipeline-provider-a-1.0.0": {"cn.vastplan.fixture.composition.provider-a", "1.0.0", "fixture.settings"},
	"pipeline-provider-b-1.0.0": {"cn.vastplan.fixture.composition.provider-b", "1.0.0", "fixture.settings"},
}

func main() {
	executable, err := os.Executable()
	if err != nil {
		log.Fatalf("读取 P5 composition fixture entry: %v", err)
	}
	name := filepath.Base(executable)
	selected, ok := identities[name]
	if !ok {
		log.Fatalf("未知 P5 composition fixture entry: %s", name)
	}
	p := sdk.New(selected.id, selected.version, map[string]string{"backend": "^0.1 || ^1.0"})
	descriptor, _ := json.Marshal(map[string]any{"title": selected.capability, "subcommands": []map[string]any{{"name": "ping", "description": "P5 runtime liveness"}}})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             selected.capability,
		Descriptor:     descriptor,
		Handlers: map[string]sdk.Handler{"ping": func(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, _ []byte) (*contractv1.CallResult, []byte, error) {
			payload, err := json.Marshal(map[string]string{"plugin": fmt.Sprintf("%s@%s", selected.id, selected.version)})
			return sdk.OK(0), payload, err
		}},
	})
	if err := p.Serve(); err != nil {
		log.Fatalf("P5 composition fixture 退出: %v", err)
	}
}
