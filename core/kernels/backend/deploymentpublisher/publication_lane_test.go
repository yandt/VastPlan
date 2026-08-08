package deploymentpublisher

import (
	"strings"
	"testing"

	compositioncommonv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/common/v1"
	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	deploymentv2 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v2"
)

func publicationLaneDeployment() deploymentv2.Deployment {
	return deploymentv2.Deployment{
		Version: 2, Revision: 1, Metadata: deploymentv1.Metadata{Name: "platform", Tenant: "local"},
		Resolution: deploymentv2.Resolution{
			PlatformProfile:        compositioncommonv1.Ref{ID: "platform", Revision: 1, Digest: strings.Repeat("a", 64)},
			ApplicationComposition: compositioncommonv1.Ref{ID: "platform", Revision: 1, Digest: strings.Repeat("b", 64)},
			PluginOrigins: map[string]string{
				"database-runtime": compositioncommonv1.OriginPlatformProfile,
				"settings":         compositioncommonv1.OriginApplication,
			},
			PluginBaselines: map[string]string{"database-runtime": "1.0.0"},
		},
		Units: []deploymentv2.ServiceUnit{
			{ID: "database", Kind: "service", Enabled: true, StartupTier: "bootstrap", Replicas: 1,
				Plugins: []deploymentv1.PluginRef{{ID: "database-runtime", Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("1", 64)}}},
			{ID: "settings", Kind: "service", Enabled: true, StartupTier: "full", Replicas: 1,
				Plugins: []deploymentv1.PluginRef{{ID: "settings", Version: "1.0.0", Channel: "stable", SHA256: strings.Repeat("2", 64)}}},
		},
	}
}

func TestApplicationPublicationCannotChangeBootstrapUnit(t *testing.T) {
	current := publicationLaneDeployment()
	next := current
	next.Revision = 2
	next.Units = append([]deploymentv2.ServiceUnit(nil), current.Units...)
	next.Units[0].Plugins = []deploymentv1.PluginRef{{ID: "database-runtime", Version: "2.0.0", Channel: "stable", SHA256: strings.Repeat("3", 64)}}
	if err := ValidatePublicationLane(PublicationLaneApplication, &current, next); err == nil {
		t.Fatal("普通发布不得修改 Bootstrap 单元")
	}
	next = current
	next.Revision = 2
	next.Units = append([]deploymentv2.ServiceUnit(nil), current.Units...)
	next.Units[1].Plugins = []deploymentv1.PluginRef{{ID: "settings", Version: "2.0.0", Channel: "stable", SHA256: strings.Repeat("4", 64)}}
	if err := ValidatePublicationLane(PublicationLaneApplication, &current, next); err != nil {
		t.Fatalf("普通发布应允许 Full 单元换版: %v", err)
	}
}

func TestBootstrapPublicationAllowsOnlyPluginVersionChanges(t *testing.T) {
	current := publicationLaneDeployment()
	next := current
	next.Revision = 2
	next.Resolution.PlatformProfile = compositioncommonv1.Ref{ID: "platform", Revision: 2, Digest: strings.Repeat("c", 64)}
	next.Resolution.PluginBaselines = map[string]string{"database-runtime": "2.0.0"}
	next.Units = append([]deploymentv2.ServiceUnit(nil), current.Units...)
	next.Units[0].Plugins = []deploymentv1.PluginRef{{ID: "database-runtime", Version: "2.0.0", Channel: "stable", SHA256: strings.Repeat("3", 64)}}
	if err := ValidatePublicationLane(PublicationLaneBootstrap, &current, next); err != nil {
		t.Fatalf("可信 Bootstrap lane 应允许精确插件换版: %v", err)
	}

	changedConfig := next
	changedConfig.Units = append([]deploymentv2.ServiceUnit(nil), next.Units...)
	changedConfig.Units[0].Config = map[string]any{"unsafe": true}
	if err := ValidatePublicationLane(PublicationLaneBootstrap, &current, changedConfig); err == nil {
		t.Fatal("Bootstrap lane 不得同时改变配置")
	}

	changedFull := next
	changedFull.Units = append([]deploymentv2.ServiceUnit(nil), next.Units...)
	changedFull.Units[1].Replicas = 2
	if err := ValidatePublicationLane(PublicationLaneBootstrap, &current, changedFull); err == nil {
		t.Fatal("Bootstrap lane 不得同时改变 Full 单元")
	}
}

func TestPublicationLanesEnforceCreationBoundary(t *testing.T) {
	next := publicationLaneDeployment()
	if err := ValidatePublicationLane(PublicationLaneApplication, nil, next); err == nil {
		t.Fatal("普通 lane 不得创建含 Bootstrap 单元的 Deployment")
	}
	if err := ValidatePublicationLane(PublicationLaneBootstrap, nil, next); err == nil {
		t.Fatal("Bootstrap lane 不得代替首次 Seed")
	}
	if err := ValidatePublicationLane(PublicationLaneSeed, nil, next); err != nil {
		t.Fatalf("可信 Seed 应允许首次发布: %v", err)
	}
}
