package deploymentmanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	backendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/backend/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func protectDeploymentTransition(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, generation uint64, previous *backendcompositionv1.ApplicationComposition, candidate backendcompositionv1.ApplicationComposition) error {
	values := []pluginv1.ArtifactReference{}
	if previous != nil {
		prior, err := compositionArtifactReferences(ctx, host, call, *previous, "active")
		if err != nil {
			return err
		}
		values = append(values, prior...)
	}
	next, err := compositionArtifactReferences(ctx, host, call, candidate, "candidate")
	if err != nil {
		return err
	}
	values = mergeArtifactReferences(values, next)
	return publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerDeploymentActive, OwnerID: "deployment/" + deployment, Generation: generation, References: values})
}

func publishDeploymentReferences(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, generation uint64, active backendcompositionv1.ApplicationComposition, rollback *backendcompositionv1.ApplicationComposition) error {
	values, err := compositionArtifactReferences(ctx, host, call, active, "active")
	if err != nil {
		return err
	}
	rollbackValues := []pluginv1.ArtifactReference{}
	if rollback != nil {
		rollbackValues, err = compositionArtifactReferences(ctx, host, call, *rollback, "rollback")
		if err != nil {
			return err
		}
	}
	if err := publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerRollbackHistory, OwnerID: "deployment/" + deployment, Generation: generation, References: rollbackValues}); err != nil {
		return err
	}
	return publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerDeploymentActive, OwnerID: "deployment/" + deployment, Generation: generation * 2, References: values})
}

func compositionArtifactReferences(ctx context.Context, host sdk.Host, call *contractv1.CallContext, composition backendcompositionv1.ApplicationComposition, purpose string) ([]pluginv1.ArtifactReference, error) {
	values := []pluginv1.ArtifactReference{}
	for _, unit := range composition.Units {
		for _, plugin := range unit.Spec.Plugins {
			channel := plugin.Channel
			if channel == "" {
				channel = "stable"
			}
			entry, err := lookupArtifactReference(ctx, host, call, pluginv1.ArtifactRef{PluginID: plugin.ID, Version: plugin.Version, Channel: channel})
			if err != nil {
				return nil, err
			}
			values = append(values, pluginv1.ArtifactReference{Ref: entry.Ref, SHA256: entry.SHA256, Purpose: purpose})
		}
	}
	return mergeArtifactReferences(nil, values), nil
}

func lookupArtifactReference(ctx context.Context, host sdk.Host, call *contractv1.CallContext, ref pluginv1.ArtifactRef) (artifactCatalogEntry, error) {
	if host == nil || call == nil {
		return artifactCatalogEntry{}, errors.New("制品引用解析缺少可信宿主")
	}
	raw, _ := json.Marshal(map[string]any{"pluginId": ref.PluginID, "version": ref.Version, "channel": ref.Channel, "target": "backend", "page": 1, "pageSize": 2})
	operation := "listCatalog"
	logicalService, routingDomain := platformadminapi.ArtifactsCapability, "platform"
	result, payload, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return artifactCatalogEntry{}, fmt.Errorf("解析受保护制品引用失败: %w", coalesceError(err, errTestArtifact))
	}
	var page artifactCatalogPage
	if json.Unmarshal(payload, &page) != nil || page.Total != 1 || len(page.Items) != 1 || page.Items[0].Ref != ref {
		return artifactCatalogEntry{}, errTestArtifact
	}
	return page.Items[0], nil
}

func publishReferenceSnapshot(ctx context.Context, host sdk.Host, call *contractv1.CallContext, value pluginv1.ArtifactReferenceSnapshot) error {
	sealed, err := artifactreference.Seal(value)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(sealed)
	if err != nil {
		return err
	}
	operation := "putReferences"
	logicalService, routingDomain := platformadminapi.ArtifactsCapability, "platform"
	result, _, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}, call, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return fmt.Errorf("提交制品引用保护失败: %w", coalesceError(err, errTestArtifact))
	}
	return nil
}

func mergeArtifactReferences(left, right []pluginv1.ArtifactReference) []pluginv1.ArtifactReference {
	byRef := map[pluginv1.ArtifactRef]pluginv1.ArtifactReference{}
	for _, value := range append(append([]pluginv1.ArtifactReference(nil), left...), right...) {
		byRef[value.Ref] = value
	}
	values := make([]pluginv1.ArtifactReference, 0, len(byRef))
	for _, value := range byRef {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Ref.PluginID != values[j].Ref.PluginID {
			return values[i].Ref.PluginID < values[j].Ref.PluginID
		}
		if values[i].Ref.Version != values[j].Ref.Version {
			return values[i].Ref.Version < values[j].Ref.Version
		}
		return values[i].Ref.Channel < values[j].Ref.Channel
	})
	return values
}
