package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/artifactreference"
	"cdsoft.com.cn/VastPlan/core/shared/go/callcontext"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/platformadminapi"
)

const (
	assignmentReferenceTTL       = 120
	assignmentReferenceHeartbeat = 40 * time.Second
	bootstrapReferenceHeartbeat  = 10 * time.Minute
)

type AddressingArtifactReferencePublisher struct {
	router          *addressing.Router
	callerID        string
	scene           string
	authenticatedBy string
}

func NewAddressingArtifactReferencePublisher(router *addressing.Router, nodeID string) (*AddressingArtifactReferencePublisher, error) {
	if router == nil || nodeID == "" {
		return nil, errors.New("Assignment 引用发布器需要 addressing router 与 node ID")
	}
	return &AddressingArtifactReferencePublisher{
		router: router, callerID: "node-agent/" + nodeID,
		scene: "artifact.references.assignment", authenticatedBy: "kernel.node-agent",
	}, nil
}

func NewBootstrapArtifactReferencePublisher(router *addressing.Router, repositoryID string) (*AddressingArtifactReferencePublisher, error) {
	if router == nil || repositoryID == "" {
		return nil, errors.New("Bootstrap 引用发布器需要 addressing router 与 repository ID")
	}
	return &AddressingArtifactReferencePublisher{
		router: router, callerID: "bootstrap-inventory/" + repositoryID,
		scene: "artifact.references.bootstrap", authenticatedBy: "kernel.bootstrap-inventory",
	}, nil
}

func (p *AddressingArtifactReferencePublisher) Publish(ctx context.Context, tenantID string, value pluginv1.ArtifactReferenceSnapshot) error {
	wire := &contractv1.CallContext{
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: p.callerID},
		Principal: &contractv1.Principal{TenantId: tenantID}, TenantId: tenantID, Scene: p.scene,
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{Source: "backend.kernel", AuthenticatedBy: p.authenticatedBy})
	if err != nil {
		return err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	operation, logicalService, routingDomain := "putReferences", platformadminapi.ArtifactsCapability, "platform"
	target := &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.ArtifactsCapability, Operation: &operation, LogicalService: &logicalService, RoutingDomain: &routingDomain}
	result, _, err := p.router.Invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), raw)
	if err != nil {
		return fmt.Errorf("路由制品引用快照: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("远端制品仓库拒绝制品引用快照")
	}
	return nil
}

func (r *Reconciler) publishBootstrapReferences(ctx context.Context, actual *ActualState) error {
	if r.BootstrapInventory == nil {
		return nil
	}
	if r.BootstrapReferences == nil {
		return nil
	}
	inventory := *r.BootstrapInventory
	if actual.BootstrapGeneration > inventory.Generation {
		return errors.New("Bootstrap Inventory generation 发生回退")
	}
	now := r.now()
	if actual.BootstrapGeneration == inventory.Generation && !actual.BootstrapPublishedAt.IsZero() && now.Sub(actual.BootstrapPublishedAt) < bootstrapReferenceHeartbeat {
		return nil
	}
	seed, err := artifactreference.Seal(inventory.SeedSnapshot())
	if err != nil {
		return err
	}
	lkg, err := artifactreference.Seal(inventory.LastKnownGoodSnapshot())
	if err != nil {
		return err
	}
	tenantID := actual.ReferenceTenant
	if tenantID == "" {
		return errors.New("Bootstrap 引用缺少期望态租户")
	}
	if err := r.BootstrapReferences.Publish(ctx, tenantID, seed); err != nil {
		return err
	}
	if err := r.BootstrapReferences.Publish(ctx, tenantID, lkg); err != nil {
		return err
	}
	actual.BootstrapGeneration = inventory.Generation
	actual.BootstrapPublishedAt = now
	return r.checkpoint(actual)
}

func (r *Reconciler) publishAssignmentReferences(ctx context.Context, desiredRevision uint64, actual *ActualState, release bool) error {
	if r.References == nil {
		return nil
	}
	tenantID, ownerID := actual.ReferenceTenant, actual.ReferenceOwnerID
	if tenantID == "" || ownerID == "" {
		if release && tenantID == "" && ownerID == "" {
			return nil
		}
		return errors.New("Assignment 引用缺少 tenant 或 owner")
	}
	now := r.now()
	due := release || actual.ReferencePending || actual.ReferenceDesiredRevision != desiredRevision || actual.ReferencePublishedAt.IsZero() || now.Sub(actual.ReferencePublishedAt) >= assignmentReferenceHeartbeat
	if !due {
		return nil
	}
	if release || !actual.ReferencePending || actual.ReferenceDesiredRevision != desiredRevision {
		actual.ReferenceGeneration++
		actual.ReferenceDesiredRevision = desiredRevision
		actual.ReferencePending = true
		if err := r.checkpoint(actual); err != nil {
			return err
		}
	}
	references, err := assignmentArtifactReferences(*actual, release)
	if err != nil {
		return err
	}
	ttl := uint32(assignmentReferenceTTL)
	if release {
		ttl = 0
	}
	snapshot, err := artifactreference.Seal(pluginv1.ArtifactReferenceSnapshot{
		OwnerKind: artifactreference.OwnerAssignmentActive, OwnerID: ownerID,
		Generation: actual.ReferenceGeneration, TTLSeconds: ttl, References: references,
	})
	if err != nil {
		return err
	}
	if err := r.References.Publish(ctx, tenantID, snapshot); err != nil {
		return err
	}
	actual.ReferencePending = false
	actual.ReferencePublishedAt = now
	if err := r.checkpoint(actual); err != nil {
		actual.ReferencePending = true
		return err
	}
	return nil
}

func assignmentArtifactReferences(actual ActualState, release bool) ([]pluginv1.ArtifactReference, error) {
	if release {
		return []pluginv1.ArtifactReference{}, nil
	}
	byRef := map[pluginv1.ArtifactRef]pluginv1.ArtifactReference{}
	for _, unit := range actual.Units {
		for _, plugin := range unit.Plugins {
			ref := pluginv1.ArtifactRef{PluginID: plugin.ID, Version: plugin.Version, Channel: plugin.Channel}
			if ref.Channel == "" {
				ref.Channel = "stable"
			}
			if prior, exists := byRef[ref]; exists && prior.SHA256 != plugin.SHA256 {
				return nil, fmt.Errorf("Assignment 精确制品 %s@%s/%s 对应多个 SHA-256", ref.PluginID, ref.Version, ref.Channel)
			}
			byRef[ref] = pluginv1.ArtifactReference{Ref: ref, SHA256: plugin.SHA256, Purpose: "active"}
		}
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
	return values, nil
}
