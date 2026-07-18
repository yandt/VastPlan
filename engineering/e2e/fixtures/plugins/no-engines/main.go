// 夹具插件：故意**不声明** backend 内核的 engines。
//
// 用于验证 ADR-0017 §4 强制点 2 的 fail-closed：未声明本内核兼容范围的插件
// 必须被拒绝装载（说明它本就不该被装进这个内核）。
//
// 这是纯测试夹具，故放 engineering/e2e/fixtures/ 而非 extensions/plugins/（ADR-0018 §3）。
package main

import (
	"log"

	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	// engines 为空：未声明对 backend 内核的兼容范围
	p := sdk.New("com.vastplan.fixture.no-engines", "0.1.0", map[string]string{})

	p.Contribute(sdk.Contribution{
		ExtensionPoint: "tool.package",
		ID:             "fixture.no-engines",
		Descriptor:     []byte(`{"title":"不该被装上的夹具"}`),
	})

	if err := p.Serve(); err != nil {
		log.Fatalf("夹具插件退出: %v", err)
	}
}
