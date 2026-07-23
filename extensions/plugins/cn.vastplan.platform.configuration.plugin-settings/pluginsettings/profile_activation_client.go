package pluginsettings

import (
	"context"
	"encoding/json"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformprofileactivation"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func createProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, definition pluginconfiguration.Definition, candidate pluginconfiguration.Candidate, stages []credentialStage) (platformprofileactivation.Activation, error) {
	credentials := make(map[string]pluginconfig.ManagedCredentialRef, len(stages))
	for _, binding := range stages {
		credentials[binding.FieldID] = binding.Stage.Ref
	}
	request := platformprofileactivation.CreateActivationRequest{
		CandidateID: candidate.ID, ConfigurationID: candidate.ConfigurationID, ConfigCatalogDigest: candidate.CatalogDigest,
		SchemaDigest: candidate.SchemaDigest, ArtifactSHA256: candidate.ArtifactSHA256,
		Values: append(json.RawMessage(nil), candidate.Values...), Credentials: credentials,
	}
	var activation platformprofileactivation.Activation
	err := callProfileActivation(ctx, host, call, platformprofileactivation.CreateActivationOperation, map[string]any{"profileActivation": request}, &activation)
	return activation, err
}

func getProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (platformprofileactivation.Activation, error) {
	var activation platformprofileactivation.Activation
	err := callProfileActivation(ctx, host, call, platformprofileactivation.GetActivationOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func approveProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (platformprofileactivation.Activation, error) {
	var activation platformprofileactivation.Activation
	err := callProfileActivation(ctx, host, call, platformprofileactivation.ApproveActivationOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func publishProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (platformprofileactivation.Activation, error) {
	var activation platformprofileactivation.Activation
	err := callProfileActivation(ctx, host, call, platformprofileactivation.PublishActivationOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func abortProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string) (platformprofileactivation.Activation, error) {
	var activation platformprofileactivation.Activation
	err := callProfileActivation(ctx, host, call, platformprofileactivation.AbortActivationOperation, map[string]string{"candidateId": candidateID}, &activation)
	return activation, err
}

func callProfileActivation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request any, activation *platformprofileactivation.Activation) error {
	if host == nil {
		return errors.New("插件配置协调器缺少可信宿主")
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}
	logicalService, routingDomain := deploymentLogicalService, "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: platformprofileactivation.DeploymentCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("部署管理器拒绝 Platform Profile 配置激活")
	}
	if err := decodeStrict(raw, activation); err != nil {
		return err
	}
	return activation.Validate()
}
