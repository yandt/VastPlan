package versionworkspace

import (
	"testing"

	resourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
)

func TestConfiguredServiceRegistersContractSurface(t *testing.T) {
	if _, err := BuildConfiguredService(StartupConfiguration{}); err == nil {
		t.Fatal("空 Environment 配置必须拒绝")
	}
	service, err := BuildConfiguredService(StartupConfiguration{Environments: []resourcev1.EnvironmentProfile{{
		Protocol: resourcev1.Protocol, ID: "platform-development", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{
			ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: JSONAdapterID,
			AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionDomainHot,
		}},
		Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 8, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	contribution := service.Contribution()
	for _, operation := range []string{
		workspacev1.OperationOpen, workspacev1.OperationStatus, workspacev1.OperationReadSnapshot, workspacev1.OperationWriteSnapshot,
		workspacev1.OperationChanges, workspacev1.OperationCommit, workspacev1.OperationDiscard, workspacev1.OperationRenew,
	} {
		if contribution.Handlers[operation] == nil {
			t.Errorf("缺少 Workspace handler %s", operation)
		}
	}
}

func TestCatalogRejectsUnsupportedBindingMode(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(NewJSONAdapter()); err != nil {
		t.Fatal(err)
	}
	err := catalog.RegisterEnvironment(resourcev1.EnvironmentProfile{
		Protocol: resourcev1.Protocol, ID: "git-by-accident", Revision: 1,
		Bindings: []resourcev1.ResourceBinding{{
			ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: JSONAdapterID,
			AllowedModes: []string{resourcev1.ModeGit}, DefaultMode: resourcev1.ModeGit, ProjectionPolicy: resourcev1.ProjectionDomainHot,
		}},
		Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 8, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
	})
	if err == nil {
		t.Fatal("JSON Adapter 不得被绑定到 Git 模式")
	}
}
