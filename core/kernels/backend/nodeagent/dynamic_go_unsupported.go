//go:build (!linux && !darwin && !freebsd) || !cgo

package nodeagent

import (
	"fmt"
	"runtime"

	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func loadDynamicGo(_, _, _, _ string) (protocolbus.EmbeddedPlugin, error) {
	return protocolbus.EmbeddedPlugin{}, fmt.Errorf("dynamic-go 不支持当前平台 %s/%s", runtime.GOOS, runtime.GOARCH)
}
