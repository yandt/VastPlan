package main

import (
	"testing"

	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
)

func TestBuildSharedStateDependencySelectsPlatformControlBinding(t *testing.T) {
	binding := sharedstate.NewBindingStore()
	got, err := buildSharedStateDependency(nil, &platformControlCoordinator{binding: binding})
	if err != nil {
		t.Fatal(err)
	}
	if got != binding {
		t.Fatalf("SQL 模式必须选择 Platform Control Binding: got %T", got)
	}
}

func TestBuildSharedStateDependencyRejectsMissingPlatformControlBinding(t *testing.T) {
	if _, err := buildSharedStateDependency(nil, &platformControlCoordinator{}); err == nil {
		t.Fatal("SQL 模式缺少 Binding 时必须失败关闭")
	}
}

func TestBuildSharedStateDependencyAllowsUnconfiguredBootstrap(t *testing.T) {
	got, err := buildSharedStateDependency(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("未配置 NATS 或 SQL 时 Shared State 应保持未注入: got %T", got)
	}
}
