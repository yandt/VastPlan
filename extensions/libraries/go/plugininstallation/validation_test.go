package plugininstallation

import (
	"errors"
	"testing"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
)

func TestValidatePreviewRequestNormalizesRequirement(t *testing.T) {
	request, err := ValidatePreviewRequest(PreviewRequest{
		Version: ProtocolVersion,
		Target:  Target{Kernel: "backend", Deployment: " services ", UnitID: " api "},
		Change: Change{Action: ActionInstall, PluginID: "cn.example.app", Requirement: &pluginv1.ArtifactRequirement{
			PluginID: "cn.example.app", Constraint: "^1.2.0", Channel: "stable", Features: []string{"trace", "audit", "trace"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if request.Target.Deployment != "services" || request.Target.UnitID != "api" || len(request.Change.Requirement.Features) != 2 || request.Change.Requirement.Features[0] != "audit" {
		t.Fatalf("安装意图没有规范化: %+v", request)
	}
}

func TestValidatePreviewRequestRejectsAmbiguousChanges(t *testing.T) {
	base := PreviewRequest{Version: ProtocolVersion, Target: Target{Kernel: "backend", Deployment: "services", UnitID: "api"}}
	tests := []PreviewRequest{
		base,
		{Version: ProtocolVersion, Target: base.Target, Change: Change{Action: ActionInstall, PluginID: "cn.example.app"}},
		{Version: ProtocolVersion, Target: base.Target, Change: Change{Action: ActionRemove, PluginID: "cn.example.app", Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.example.app", Constraint: "=1.0.0"}}},
		{Version: ProtocolVersion, Target: base.Target, Change: Change{Action: ActionUpgrade, PluginID: "cn.example.app", Requirement: &pluginv1.ArtifactRequirement{PluginID: "cn.example.other", Constraint: "=1.0.0"}}},
	}
	for _, test := range tests {
		if _, err := ValidatePreviewRequest(test); !errors.Is(err, ErrInvalid) {
			t.Fatalf("无歧义校验应拒绝 %+v: %v", test, err)
		}
	}
}
