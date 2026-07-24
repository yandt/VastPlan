package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureFrozenNodeDependenciesUsesOfflineFrozenInstall(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "pnpm-lock.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	err := ensureFrozenNodeDependencies(context.Background(), root, func(_ context.Context, gotRoot string) ([]byte, error) {
		called = true
		if gotRoot != root {
			t.Fatalf("依赖对齐目录错误: got=%s want=%s", gotRoot, root)
		}
		return []byte("already up to date"), nil
	})
	if err != nil || !called {
		t.Fatalf("冻结依赖对齐应成功: called=%t err=%v", called, err)
	}
}

func TestEnsureFrozenNodeDependenciesFailsClosedWithRecoveryCommand(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"package.json", "pnpm-lock.yaml"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	err := ensureFrozenNodeDependencies(context.Background(), root, func(context.Context, string) ([]byte, error) {
		return []byte("package is missing from the store"), errors.New("exit status 1")
	})
	if err == nil || !strings.Contains(err.Error(), "pnpm install --frozen-lockfile") || !strings.Contains(err.Error(), "missing from the store") {
		t.Fatalf("失败信息必须保留原因和恢复命令: %v", err)
	}
}

func TestEnsureFrozenNodeDependenciesRequiresWorkspaceLock(t *testing.T) {
	called := false
	err := ensureFrozenNodeDependencies(context.Background(), t.TempDir(), func(context.Context, string) ([]byte, error) {
		called = true
		return nil, nil
	})
	if err == nil || called {
		t.Fatalf("缺少锁文件时必须在执行 pnpm 前失败: called=%t err=%v", called, err)
	}
}
