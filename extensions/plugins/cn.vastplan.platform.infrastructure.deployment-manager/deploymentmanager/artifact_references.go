package deploymentmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func protectDeploymentTransition(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, generation uint64, previous, candidate []pluginv1.ArtifactReference) error {
	values := mergeArtifactReferences(withArtifactPurpose(previous, "active"), withArtifactPurpose(candidate, "candidate"))
	return publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerDeploymentActive, OwnerID: "deployment/" + deployment, Generation: generation, References: values})
}

func publishDeploymentReferences(ctx context.Context, host sdk.Host, call *contractv1.CallContext, deployment string, generation uint64, active, rollback []pluginv1.ArtifactReference) error {
	values := withArtifactPurpose(active, "active")
	rollbackValues := withArtifactPurpose(rollback, "rollback")
	if err := publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerRollbackHistory, OwnerID: "deployment/" + deployment, Generation: generation, References: rollbackValues}); err != nil {
		return err
	}
	return publishReferenceSnapshot(ctx, host, call, pluginv1.ArtifactReferenceSnapshot{OwnerKind: artifactreference.OwnerDeploymentActive, OwnerID: "deployment/" + deployment, Generation: generation * 2, References: values})
}

func withArtifactPurpose(values []pluginv1.ArtifactReference, purpose string) []pluginv1.ArtifactReference {
	out := make([]pluginv1.ArtifactReference, len(values))
	for i, value := range values {
		out[i] = value
		out[i].Purpose = purpose
	}
	return out
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
