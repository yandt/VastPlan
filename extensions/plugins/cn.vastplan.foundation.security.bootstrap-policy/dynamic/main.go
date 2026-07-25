// Package main 导出 bootstrap-policy 的 dynamic-go ABI 入口，不承载策略分支。
package main

import (
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/dynamicgo"
)

// dynamicGoBuildFingerprint 必须由官方构建命令使用 -X 注入；默认空值会被宿主拒绝。
var dynamicGoBuildFingerprint string

// VastPlanDynamicGo 是 protocolbus.DynamicGoSymbol 规定的唯一导出入口。
func VastPlanDynamicGo() dynamicgo.Module {
	return dynamicgo.Module{
		ABI: dynamicgo.ABIV1, BuildFingerprint: dynamicGoBuildFingerprint,
		Plugin: definition(),
	}
}

func main() {}
