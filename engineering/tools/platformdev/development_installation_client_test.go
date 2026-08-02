package main

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/engineering/internal/pluginlibrarysource"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

type developmentInstallationInvoker struct {
	operations []string
	request    plugininstallation.PreviewRequest
}

func (f *developmentInstallationInvoker) Invoke(_ context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, raw []byte) (*contractv1.CallResult, []byte, error) {
	f.operations = append(f.operations, target.GetOperation())
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_SYSTEM || call.GetCaller().GetId() != developmentInstallationCaller {
		return nil, nil, plugininstallation.ErrUntrustedSource
	}
	if target.GetOperation() == "listTestTargetBindings" {
		return successfulCapabilityResponse(map[string]any{"items": []platformadminapi.TestTargetBinding{
			{ID: "blocked", Kind: platformadminapi.TestTargetBackend, Deployment: "services", UnitID: "api", PluginID: "cn.example.app", Enabled: true},
			{ID: "enabled", Kind: platformadminapi.TestTargetBackend, Deployment: "services", UnitID: "worker", PluginID: "cn.example.app", Enabled: true, AllowInstall: true, PortalTargets: []string{"operations"}},
		}})
	}
	var envelope struct {
		InstallationPreview plugininstallation.PreviewRequest `json:"installationPreview"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, nil, err
	}
	f.request = envelope.InstallationPreview
	return successfulCapabilityResponse(plugininstallation.Candidate{ID: "installation-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: plugininstallation.CandidateReady})
}

func TestDevelopmentInstallationClientUsesOnlyExplicitInstallBindings(t *testing.T) {
	invoker := &developmentInstallationInvoker{}
	client := developmentInstallationClient{invoker: invoker, logf: func(string, ...any) {}}
	ref := pluginv1.ArtifactRef{PluginID: "cn.example.app", Version: "1.2.3-dev.1", Channel: "workspace"}
	if err := client.ApplyInstallationIntent(context.Background(), pluginlibrarysource.InstallationIntent{Action: plugininstallation.ActionInstall, PluginID: ref.PluginID, Artifact: &ref}); err != nil {
		t.Fatal(err)
	}
	if len(invoker.operations) != 2 || invoker.operations[1] != plugininstallation.DevelopmentApplyOperation {
		t.Fatalf("只应对 allowInstall 绑定应用一次开发意图: %v", invoker.operations)
	}
	if invoker.request.Target.UnitID != "worker" || len(invoker.request.PortalTargets) != 1 || invoker.request.PortalTargets[0] != "operations" {
		t.Fatalf("开发安装目标未从显式绑定派生: %+v", invoker.request)
	}
}

func successfulCapabilityResponse(value any) (*contractv1.CallResult, []byte, error) {
	raw, err := json.Marshal(value)
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
}
