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
		workspacev1.OperationDescribeResource,
		workspacev1.OperationOpen, workspacev1.OperationStatus, workspacev1.OperationReadSnapshot, workspacev1.OperationWriteSnapshot,
		workspacev1.OperationChanges, workspacev1.OperationCommit, workspacev1.OperationDiscard, workspacev1.OperationRenew,
		workspacev1.OperationReadCommitted, workspacev1.OperationCompareCommitted,
		workspacev1.OperationBeginContentUpload, workspacev1.OperationContentUploadStatus, workspacev1.OperationRenewContentUpload,
		workspacev1.OperationCompleteContentUpload, workspacev1.OperationAbortContentUpload,
	} {
		if contribution.Handlers[operation] == nil {
			t.Errorf("缺少 Workspace handler %s", operation)
		}
	}
}

func TestConfiguredServiceRegistersStandardFilesAdapters(t *testing.T) {
	for _, adapterID := range []string{TextAdapterID, BlobAdapterID, FilesAdapterID} {
		_, err := BuildConfiguredService(StartupConfiguration{Environments: []resourcev1.EnvironmentProfile{{
			Protocol: resourcev1.Protocol, ID: "content-development", Revision: 1,
			Bindings: []resourcev1.ResourceBinding{{
				ResourceType: "script.bundle", Namespace: "script.bundle", Adapter: adapterID,
				AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionNone,
			}},
			Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: 8, MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 64 << 20},
		}}})
		if err != nil {
			t.Fatalf("标准 Adapter %s 未注册: %v", adapterID, err)
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

func TestCatalogRetainsEnvironmentRevisionsAndSelectsLatest(t *testing.T) {
	catalog := NewCatalog()
	if err := catalog.RegisterAdapter(NewJSONAdapter()); err != nil {
		t.Fatal(err)
	}
	profile := func(revision uint64) resourcev1.EnvironmentProfile {
		return resourcev1.EnvironmentProfile{
			Protocol: resourcev1.Protocol, ID: "platform-development", Revision: revision,
			Bindings: []resourcev1.ResourceBinding{{
				ResourceType: "portal.configuration", Namespace: "portal.configuration", Adapter: JSONAdapterID,
				AllowedModes: []string{resourcev1.ModeSnapshot}, DefaultMode: resourcev1.ModeSnapshot, ProjectionPolicy: resourcev1.ProjectionDomainHot,
			}},
			Limits: resourcev1.WorkspaceLimits{MaxSessionsPerTenant: int(7 + revision), MaxLeaseSeconds: 3600, MaxSnapshotBytes: 1 << 20, MaxOverlayBytes: 1 << 20},
		}
	}
	newer, older := profile(2), profile(1)
	newerDigest, _ := resourcev1.EnvironmentDigest(newer)
	olderDigest, _ := resourcev1.EnvironmentDigest(older)
	if err := catalog.RegisterEnvironment(newer); err != nil {
		t.Fatal(err)
	}
	if err := catalog.RegisterEnvironment(older); err != nil {
		t.Fatal(err)
	}
	current, _, _, err := catalog.resolve("platform-development", "portal.configuration")
	if err != nil || current.digest != newerDigest {
		t.Fatalf("当前环境未选择最高 revision: digest=%s err=%v", current.digest, err)
	}
	historical, _, _, err := catalog.resolveExact("platform-development", olderDigest, "portal.configuration")
	if err != nil || historical.profile.Revision != 1 || historical.digest != olderDigest {
		t.Fatalf("无法按摘要解析历史环境: %+v err=%v", historical.profile, err)
	}
	if err := catalog.RegisterEnvironment(older); err == nil {
		t.Fatal("相同 revision 不得重复注册")
	}
}
