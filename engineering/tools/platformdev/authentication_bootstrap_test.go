package main

import (
	"os"
	"path/filepath"
	"testing"

	broker "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication-broker/broker"
)

func TestDevelopmentManagementStateWritesOwnerOnlyAndRejectsInvalidStateAtomically(t *testing.T) {
	path := filepath.Join(t.TempDir(), "provider-state.json")
	state := broker.ManagementState{Version: 1, Generation: 1, Providers: []broker.ManagedProvider{}}
	if _, err := compareAndSwapDevelopmentManagementState(path, 0, state); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("开发 Authentication 状态必须作为 owner-only 普通文件提交: info=%v err=%v", info, err)
	}
	invalid := state
	invalid.Generation++
	invalid.Version = 0
	if _, err := compareAndSwapDevelopmentManagementState(path, state.Generation, invalid); err == nil {
		t.Fatal("无效 Management State 不得提交")
	}
	reloaded, err := (&broker.FileManagementStateReader{Path: path}).LoadState()
	if err != nil || reloaded.Generation != state.Generation {
		t.Fatalf("无效写入不得替换已提交 Management State: %+v err=%v", reloaded, err)
	}
}
