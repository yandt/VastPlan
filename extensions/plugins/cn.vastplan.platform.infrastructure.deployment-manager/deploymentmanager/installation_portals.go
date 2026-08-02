package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func prepareInstallationPortals(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidate plugininstallation.Candidate) ([]string, []string, error) {
	portals := sortedPortalTargets(candidate.Preview.PortalTargets)
	prepared := make([]string, 0, len(portals))
	committed := make([]string, 0, len(portals))
	request := portalapi.PluginInstallationRequest{
		CandidateID: candidate.ID, Action: candidate.Preview.Action, PluginID: candidate.Preview.PluginID,
		Artifact: installationPortalArtifact(candidate),
	}
	for _, portalID := range portals {
		request.PortalID = portalID
		var result portalapi.PluginInstallationPreparation
		if err := callPortalInstallation(ctx, host, call, portalapi.PreparePluginInstallationOperation, request, &result); err != nil {
			abortInstallationPortals(ctx, host, call, candidate.ID, prepared)
			return nil, committed, fmt.Errorf("预热 Portal %s: %w", portalID, err)
		}
		if result.Status != portalapi.PluginInstallationPrepared && result.Status != portalapi.PluginInstallationCommitted {
			abortInstallationPortals(ctx, host, call, candidate.ID, prepared)
			return nil, committed, fmt.Errorf("Portal %s 未进入 Prepared/Committed", portalID)
		}
		if result.Status == portalapi.PluginInstallationCommitted {
			committed = append(committed, portalID)
		} else {
			prepared = append(prepared, portalID)
		}
	}
	return portals, committed, nil
}

func commitInstallationPortals(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string, portals []string) ([]string, error) {
	committed := make([]string, 0, len(portals))
	for _, portalID := range portals {
		var result portalapi.PluginInstallationPreparation
		lookup := portalapi.PluginInstallationLookup{CandidateID: candidateID, PortalID: portalID}
		if err := callPortalInstallation(ctx, host, call, portalapi.CommitPluginInstallationOperation, lookup, &result); err != nil {
			return committed, fmt.Errorf("提交 Portal %s: %w", portalID, err)
		}
		if result.Status != portalapi.PluginInstallationCommitted {
			return committed, fmt.Errorf("Portal %s 未进入 Committed", portalID)
		}
		committed = append(committed, portalID)
	}
	return committed, nil
}

func abortInstallationPortals(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string, portals []string) {
	for index := len(portals) - 1; index >= 0; index-- {
		lookup := portalapi.PluginInstallationLookup{CandidateID: candidateID, PortalID: portals[index]}
		_ = callPortalInstallation(ctx, host, call, portalapi.AbortPluginInstallationOperation, lookup, nil)
	}
}

func rollbackInstallationPortals(ctx context.Context, host sdk.Host, call *contractv1.CallContext, candidateID string, portals []string) error {
	var rollbackErr error
	for index := len(portals) - 1; index >= 0; index-- {
		lookup := portalapi.PluginInstallationLookup{CandidateID: candidateID, PortalID: portals[index]}
		rollbackErr = errors.Join(rollbackErr, callPortalInstallation(ctx, host, call, portalapi.RollbackPluginInstallationOperation, lookup, nil))
	}
	return rollbackErr
}

func callPortalInstallation(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, payload, output any) error {
	if host == nil || call == nil {
		return errors.New("Portal 安装调用上下文不完整")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "platform.portal-composer", "platform"
	result, response, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: portalapi.ComposerCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, raw)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
		}
		return errors.New("Portal 安装能力返回非成功状态")
	}
	if output != nil {
		if err := json.Unmarshal(response, output); err != nil {
			return err
		}
	}
	return nil
}

func installationPortalArtifact(candidate plugininstallation.Candidate) *pluginv1.ArtifactRef {
	if candidate.Preview.Action == plugininstallation.ActionRemove || candidate.Preview.ArtifactLock == nil {
		return nil
	}
	for _, item := range candidate.Preview.ArtifactLock.Packages {
		if item.Ref.PluginID == candidate.Preview.PluginID {
			value := item.Ref
			return &value
		}
	}
	return nil
}

func sortedPortalTargets(values []string) []string {
	result := append([]string(nil), values...)
	sort.Strings(result)
	return result
}
