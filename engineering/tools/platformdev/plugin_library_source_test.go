package main

import (
	"context"
	"errors"
	"testing"

	artifactrepositoryv1 "cdsoft.com.cn/VastPlan/contracts/schemas/artifactrepository/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

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
