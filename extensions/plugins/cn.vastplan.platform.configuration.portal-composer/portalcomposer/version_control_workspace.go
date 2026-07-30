package portalcomposer

import (
	"context"
	"encoding/json"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	versionresourcev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionresource/v1"
	workspacev1 "cdsoft.com.cn/VastPlan/contracts/schemas/versionworkspace/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	workspaceclient "cdsoft.com.cn/VastPlan/extensions/sdk/go/versionworkspace"
)

type workspacePortalVersionControl struct {
	client  *workspaceclient.Client
	callCtx *contractv1.CallContext
}

func newWorkspacePortalVersionControl(host sdk.Host, callCtx *contractv1.CallContext) (PortalVersionControl, error) {
	client, err := workspaceclient.New(host)
	if err != nil {
		return nil, err
	}
	if callCtx == nil {
		return nil, fmt.Errorf("Version Workspace 调用缺少可信上下文")
	}
	return &workspacePortalVersionControl{client: client, callCtx: callCtx}, nil
}

func (c *workspacePortalVersionControl) Describe(ctx context.Context, binding PortalVersionControlBinding, portalID string) (PortalVersionControlCapabilities, error) {
	if err := binding.validate(); err != nil {
		return PortalVersionControlCapabilities{}, err
	}
	description, err := c.client.DescribeResource(ctx, c.callCtx, workspacev1.DescribeResourceRequest{
		EnvironmentID: binding.EnvironmentID,
		Resource:      versionresourcev1.ResourceKey{Type: binding.ResourceType, ID: portalID},
		RequestedMode: versionresourcev1.ModeSnapshot,
	})
	if err != nil {
		return PortalVersionControlCapabilities{}, versionControlCallError(err)
	}
	if description.ContentKind != versionresourcev1.ContentJSON || !description.Capabilities.Normalize {
		return PortalVersionControlCapabilities{}, fmt.Errorf("%w: Portal 配置要求可规范化 JSON Snapshot", ErrVersionControlUnavailable)
	}
	return PortalVersionControlCapabilities{Read: true, Diff: description.Capabilities.Diff, Restore: true}, nil
}

func (c *workspacePortalVersionControl) Commit(ctx context.Context, request PortalVersionCommitRequest) (PortalVersionCommitResult, error) {
	capabilities, err := c.Describe(ctx, request.Binding, request.PortalID)
	if err != nil {
		return PortalVersionCommitResult{}, err
	}
	if request.OperationID == "" {
		return PortalVersionCommitResult{}, fmt.Errorf("Portal 版本提交缺少 operationId")
	}
	session, err := c.client.Open(ctx, c.callCtx, workspacev1.OpenRequest{
		EnvironmentID: request.Binding.EnvironmentID,
		Resource:      versionresourcev1.ResourceKey{Type: request.Binding.ResourceType, ID: request.PortalID},
		RequestedMode: versionresourcev1.ModeSnapshot,
		BaseRef:       request.BaseRef,
		ReadOnly:      false,
	})
	if err != nil {
		return PortalVersionCommitResult{}, versionControlCallError(err)
	}
	raw, err := json.Marshal(request.Configuration)
	if err != nil {
		return PortalVersionCommitResult{}, err
	}
	session, err = c.client.WriteSnapshot(ctx, c.callCtx, workspacev1.WriteSnapshotRequest{
		SessionID: session.ID, ExpectedRevision: session.Revision,
		Snapshot: versionresourcev1.Snapshot{Kind: versionresourcev1.ContentJSON, MediaType: "application/json", JSON: raw},
	})
	if err != nil {
		c.discard(ctx, session)
		return PortalVersionCommitResult{}, versionControlCallError(err)
	}
	committed, err := c.client.Commit(ctx, c.callCtx, workspacev1.CommitRequest{
		SessionID: session.ID, ExpectedRevision: session.Revision, OperationID: request.OperationID,
		Message: "Submit Portal publication", Labels: map[string]string{"domain": "portal", "portalId": request.PortalID},
	})
	if err != nil {
		c.discard(ctx, session)
		return PortalVersionCommitResult{}, versionControlCallError(err)
	}
	return PortalVersionCommitResult{
		EnvironmentDigest: committed.Session.EnvironmentDigest,
		VersionRef:        committed.Version.Ref,
		Capabilities:      capabilities,
	}, nil
}

func (c *workspacePortalVersionControl) Read(ctx context.Context, request PortalVersionReadRequest) (portalapi.PortalConfiguration, error) {
	result, err := c.client.ReadCommitted(ctx, c.callCtx, workspacev1.CommittedRequest{
		EnvironmentID: request.Binding.EnvironmentID, EnvironmentDigest: request.EnvironmentDigest,
		Resource:      versionresourcev1.ResourceKey{Type: request.Binding.ResourceType, ID: request.PortalID},
		RequestedMode: versionresourcev1.ModeSnapshot, Ref: request.VersionRef,
	})
	if err != nil {
		return portalapi.PortalConfiguration{}, versionControlCallError(err)
	}
	if result.Snapshot.Kind != versionresourcev1.ContentJSON {
		return portalapi.PortalConfiguration{}, fmt.Errorf("%w: Portal 历史不是 JSON Snapshot", ErrVersionControlUnavailable)
	}
	var configuration portalapi.PortalConfiguration
	if err := decodeComposerJSON(result.Snapshot.JSON, &configuration); err != nil {
		return portalapi.PortalConfiguration{}, fmt.Errorf("Portal 历史配置无效: %w", err)
	}
	return configuration, nil
}

func (c *workspacePortalVersionControl) Compare(ctx context.Context, request PortalVersionCompareRequest) (PortalVersionCompareResult, error) {
	result, err := c.client.CompareCommitted(ctx, c.callCtx, workspacev1.CompareCommittedRequest{
		EnvironmentID: request.Binding.EnvironmentID, EnvironmentDigest: request.EnvironmentDigest,
		Resource:      versionresourcev1.ResourceKey{Type: request.Binding.ResourceType, ID: request.PortalID},
		RequestedMode: versionresourcev1.ModeSnapshot, Left: request.Left, Right: request.Right,
	})
	if err != nil {
		return PortalVersionCompareResult{}, versionControlCallError(err)
	}
	return PortalVersionCompareResult{
		Dirty: result.Dirty, DiffAvailable: result.DiffAvailable,
		ChangedPaths: append([]string(nil), result.ChangedPaths...), Summary: result.Summary,
	}, nil
}

func (c *workspacePortalVersionControl) discard(ctx context.Context, session workspacev1.Session) {
	_, _ = c.client.Discard(ctx, c.callCtx, workspacev1.RevisionRequest{SessionID: session.ID, ExpectedRevision: session.Revision})
}

func versionControlCallError(err error) error {
	if err == nil {
		return ErrVersionControlUnavailable
	}
	return fmt.Errorf("%w: %v", ErrVersionControlUnavailable, err)
}
