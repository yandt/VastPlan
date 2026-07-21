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
)

type AddressingArtifactReferencePublisher struct {
	router *addressing.Router
	nodeID string
}

func NewAddressingArtifactReferencePublisher(router *addressing.Router, nodeID string) (*AddressingArtifactReferencePublisher, error) {
	if router == nil || nodeID == "" {
		return nil, errors.New("Assignment 引用发布器需要 addressing router 与 node ID")
	}
	return &AddressingArtifactReferencePublisher{router: router, nodeID: nodeID}, nil
}

func (p *AddressingArtifactReferencePublisher) Publish(ctx context.Context, tenantID string, value pluginv1.ArtifactReferenceSnapshot) error {
	wire := &contractv1.CallContext{
		Caller:    &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "node-agent/" + p.nodeID},
		Principal: &contractv1.Principal{TenantId: tenantID}, TenantId: tenantID, Scene: "node.assignment.references",
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{Source: "node.agent", AuthenticatedBy: "kernel.assignment"})
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
		return fmt.Errorf("路由 Assignment 引用快照: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("远端制品仓库拒绝 Assignment 引用快照")
	}
	return nil
}

func (r *Reconciler) publishAssignmentReferences(ctx context.Context, desiredRevision uint64, actual *ActualState, release bool) error {
	if r.References == nil {
		return nil
	}
	tenantID, ownerID := actual.ReferenceTenant, actual.ReferenceOwnerID
	if tenantID == "" || ownerID == "" {
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
