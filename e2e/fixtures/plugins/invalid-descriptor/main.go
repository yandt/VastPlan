// invalid-descriptor 是协议注册校验的 E2E 夹具：它故意声明非法 Hook phase。
// 夹具不属于产品插件，故放在 e2e/fixtures 而非 plugins/（ADR-0018）。
package main

import (
	"log"

	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
)

func main() {
	p := sdk.New("fixture.invalid-descriptor", "0.1.0", map[string]string{"backend": "^0.1"})
	p.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.Hook,
		ID:             "fixture.invalid-hook",
		// phase 不能取 later；宿主必须在 RegisterContributions 时拒绝，
		// 而不能把这条不可能被正确分发的贡献写进 Registry。
		Descriptor: []byte(`{"point":"invoke","phase":"later"}`),
		Handlers:   map[string]sdk.Handler{},
	})
	if err := p.Serve(); err != nil {
		log.Fatalf("夹具插件退出: %v", err)
	}
}
