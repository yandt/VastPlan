package main

import (
	"context"
	"errors"
	"testing"
	"time"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestWaitForPluginLibraryRepositoryRecoversStartupRace(t *testing.T) {
	attempts := 0
	err := waitForPluginLibraryRepository(context.Background(), time.Millisecond, func(context.Context) error {
		attempts++
		if attempts < 3 {
			return errors.New("repository socket 尚未创建")
		}
		return nil
	})
	if err != nil || attempts != 3 {
		t.Fatalf("仓库就绪后应自动继续: attempts=%d err=%v", attempts, err)
	}
}

func TestWaitForPluginLibraryRepositoryStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitForPluginLibraryRepository(ctx, time.Hour, func(context.Context) error {
		return errors.New("repository unavailable")
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("停止时必须返回 context canceled: %v", err)
	}
}

type workspaceClientStub struct {
	withdrawErr error
	snapshot    artifactrepositoryv1.CatalogSnapshot
	listErr     error
}

func (s workspaceClientStub) WithdrawWorkspace(context.Context, pluginv1.ArtifactRef) (artifactrepositoryv1.WorkspaceWithdrawalRecord, error) {
	return artifactrepositoryv1.WorkspaceWithdrawalRecord{}, s.withdrawErr
}

func (s workspaceClientStub) CatalogSnapshot(context.Context) (artifactrepositoryv1.CatalogSnapshot, error) {
	return s.snapshot, s.listErr
}

func TestLocalWorkspaceWithdrawalIsIdempotentOnlyAfterCatalogConfirmation(t *testing.T) {
	ref := pluginv1.ArtifactRef{PluginID: "cn.vastplan.test", Version: "1.0.0-dev.workspace.abc", Channel: "workspace"}
	rejected := errors.New("workspace withdrawal rejected")
	withdrawer := localWorkspaceWithdrawer{client: workspaceClientStub{withdrawErr: rejected}}
	if err := withdrawer.WithdrawWorkspace(context.Background(), ref); err != nil {
		t.Fatalf("Catalog 已确认精确引用不存在时应视为撤回完成: %v", err)
	}
	withdrawer.client = workspaceClientStub{withdrawErr: rejected, snapshot: artifactrepositoryv1.CatalogSnapshot{Items: []artifactrepositoryv1.Receipt{{Ref: ref}}}}
	if err := withdrawer.WithdrawWorkspace(context.Background(), ref); !errors.Is(err, rejected) {
		t.Fatalf("精确引用仍存在时必须保留原始失败: %v", err)
	}
	listErr := errors.New("catalog unavailable")
	withdrawer.client = workspaceClientStub{withdrawErr: rejected, listErr: listErr}
	if err := withdrawer.WithdrawWorkspace(context.Background(), ref); !errors.Is(err, rejected) || !errors.Is(err, listErr) {
		t.Fatalf("Catalog 无法复核时必须 fail closed: %v", err)
	}
}
