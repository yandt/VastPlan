package configurationcontroller

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	configurationv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configuration/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type fakeController struct{}

func (fakeController) Prepare(_ context.Context, _ sdk.Host, _ *contractv1.CallContext, request configurationv1.PrepareRequest) (configurationv1.Observation, error) {
	digest, _ := configurationv1.DigestPrepareRequest(request)
	configurationDigest, _ := configurationv1.DigestConfiguration(request.Values, request.ManagedCredentials)
	return configurationv1.Observation{
		Protocol: configurationv1.Protocol, ConfigurationID: request.ConfigurationID, Active: request.ExpectedActive,
		Candidate:  &configurationv1.CandidateObservation{CandidateID: request.CandidateID, RequestDigest: digest, ConfigurationDigest: configurationDigest, Status: configurationv1.StatusPrepared, Ready: true},
		ObservedAt: time.Now().UTC(),
	}, nil
}

func (fakeController) Commit(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	return configurationv1.Observation{}, nil
}
func (fakeController) Abort(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.CandidateRequest) (configurationv1.Observation, error) {
	return configurationv1.Observation{}, nil
}
func (fakeController) Status(context.Context, sdk.Host, *contractv1.CallContext, configurationv1.StatusRequest) (configurationv1.Observation, error) {
	return configurationv1.Observation{}, nil
}

func TestContributionUsesOpaqueCapabilityAndExactCoordinator(t *testing.T) {
	const pluginID = "cn.vastplan.example.hot-controller"
	contribution, err := Contribution(pluginID, fakeController{})
	if err != nil {
		t.Fatal(err)
	}
	want, _ := pluginv1.ConfigurationControllerCapability(pluginID)
	if contribution.ExtensionPoint != configurationv1.ExtensionPoint || contribution.ID != want || strings.Contains(contribution.ID, pluginID) {
		t.Fatalf("controller contribution 身份错误: %+v", contribution)
	}
	request := configurationv1.PrepareRequest{
		CandidateID: "pcfg_" + strings.Repeat("a", 32), ConfigurationID: "cfg_" + strings.Repeat("b", 24),
		CatalogDigest: strings.Repeat("c", 64), SchemaDigest: strings.Repeat("d", 64), ArtifactSHA256: strings.Repeat("e", 64),
		ExpectedActive: configurationv1.ActiveReference{Revision: 1, Digest: strings.Repeat("f", 64)}, Values: json.RawMessage(`{"enabled":true}`),
	}
	payload, _ := json.Marshal(request)
	operation := configurationv1.OperationPrepare
	handler := contribution.Handlers[operation]
	directUser := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_USER, Id: "alice"}}
	result, _, err := handler(context.Background(), nil, directUser, payload)
	if err != nil || result.GetError().GetCode() != "configuration.controller.permission_denied" {
		t.Fatalf("浏览器用户不得直接调用 controller: result=%+v err=%v", result, err)
	}
	coordinator := &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.platform.configuration.plugin-settings"}}
	result, raw, err := handler(context.Background(), nil, coordinator, payload)
	if err != nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("有效协调器调用失败: result=%+v err=%v", result, err)
	}
	var observation configurationv1.Observation
	if json.Unmarshal(raw, &observation) != nil || observation.Candidate == nil || observation.Candidate.Status != configurationv1.StatusPrepared {
		t.Fatalf("controller observation 错误: %+v", observation)
	}
}
