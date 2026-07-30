package versionledger

import "testing"

func TestRegistrySeparatesProviderTypeFromInstance(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register("primary", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register("migration-target", NewMemoryProvider()); err != nil {
		t.Fatal(err)
	}
	if len(registry.Descriptors()) != 1 {
		t.Fatal("同一 Provider 类型的多个实例应只公开一个类型描述")
	}
	if _, ok := registry.Resolve("primary"); !ok {
		t.Fatal("无法按实例 ID 解析 Provider")
	}
	if err := registry.Register("primary", NewMemoryProvider()); err == nil {
		t.Fatal("重复 Provider instance 必须拒绝")
	}
}
