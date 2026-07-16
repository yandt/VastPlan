// Package main 是迁移 E2E 中保持旧状态视图的 v1 基线插件夹具。
package main

import (
	"fmt"
	"os"

	"cdsoft.com.cn/VastPlan/sdk/go/plugin"
)

func main() {
	p := plugin.New("com.vastplan.fixture.migrator", "1.0.0", map[string]string{"backend": "^0.1 || ^1.0"})
	if err := p.Serve(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
