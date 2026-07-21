package edge

import (
	"context"
	"encoding/json"
	"fmt"

	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// CapabilityClient is the Edge-side port for a plugin capability. The Backend
// composition root maps it to protocolbus/addressing; Edge itself never knows
// whether the capability is local, clustered, process-hosted, or embedded.
type CapabilityClient interface {
	Call(context.Context, portalapi.Principal, string, []byte) ([]byte, error)
}

// CapabilityError preserves a stable kernel application-error code across the
// Edge adapter. The HTTP handler can safely map only known codes; its message
// is diagnostic text, never an authorization decision.
type CapabilityError struct {
	Code    string
	Message string
}

func (e *CapabilityError) Error() string {
	if e == nil {
		return "Portal capability 调用失败"
	}
	if e.Message == "" {
		return fmt.Sprintf("Portal capability 调用失败: %s", e.Code)
	}
	return fmt.Sprintf("Portal capability 调用失败 [%s]: %s", e.Code, e.Message)
}

type CapabilityService struct{ client CapabilityClient }

func NewCapabilityService(client CapabilityClient) (*CapabilityService, error) {
	if client == nil {
		return nil, fmt.Errorf("Portal capability client 不能为空")
	}
	return &CapabilityService{client: client}, nil
}

func (s *CapabilityService) CreateDraft(ctx context.Context, p portalapi.Principal, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "createDraft", composition)
}
func (s *CapabilityService) UpdateDraft(ctx context.Context, p portalapi.Principal, id uint64, composition frontendcompositionv1.ApplicationComposition) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "updateDraft", struct {
		RevisionID  uint64                                       `json:"revisionId"`
		Composition frontendcompositionv1.ApplicationComposition `json:"composition"`
	}{RevisionID: id, Composition: composition})
}
func (s *CapabilityService) List(ctx context.Context, p portalapi.Principal) ([]portalapi.Revision, error) {
	return call[[]portalapi.Revision](ctx, s.client, p, "list", struct{}{})
}
func (s *CapabilityService) Submit(ctx context.Context, p portalapi.Principal, id uint64) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "submit", revisionRequest{RevisionID: id})
}
func (s *CapabilityService) Approve(ctx context.Context, p portalapi.Principal, id uint64) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "approve", revisionRequest{RevisionID: id})
}
func (s *CapabilityService) Publish(ctx context.Context, p portalapi.Principal, id uint64, request portalapi.PublishRequest) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "publish", revisionRequest{RevisionID: id, PublishRequest: request})
}
func (s *CapabilityService) Audit(ctx context.Context, p portalapi.Principal, id uint64) ([]portalapi.AuditEvent, error) {
	return call[[]portalapi.AuditEvent](ctx, s.client, p, "audit", revisionRequest{RevisionID: id})
}

func (s *CapabilityService) Governance(ctx context.Context, p portalapi.Principal) (portalapi.GovernanceSnapshot, error) {
	return call[portalapi.GovernanceSnapshot](ctx, s.client, p, "governance", struct{}{})
}
func (s *CapabilityService) CreateProfileDraft(ctx context.Context, p portalapi.Principal, profile frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	return call[portalapi.PlatformProfileRevision](ctx, s.client, p, "createProfileDraft", profile)
}
func (s *CapabilityService) UpdateProfileDraft(ctx context.Context, p portalapi.Principal, id uint64, profile frontendcompositionv1.PlatformProfile) (portalapi.PlatformProfileRevision, error) {
	return call[portalapi.PlatformProfileRevision](ctx, s.client, p, "updateProfileDraft", struct {
		RevisionID uint64                                `json:"revisionId"`
		Profile    frontendcompositionv1.PlatformProfile `json:"profile"`
	}{id, profile})
}
func (s *CapabilityService) TransitionProfile(ctx context.Context, p portalapi.Principal, id uint64, action string) (portalapi.PlatformProfileRevision, error) {
	return call[portalapi.PlatformProfileRevision](ctx, s.client, p, "transitionProfile", struct {
		RevisionID uint64 `json:"revisionId"`
		Action     string `json:"action"`
	}{id, action})
}
func (s *CapabilityService) CreateBindingDraft(ctx context.Context, p portalapi.Principal, request portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	return call[portalapi.BindingRevision](ctx, s.client, p, "createBindingDraft", request)
}
func (s *CapabilityService) UpdateBindingDraft(ctx context.Context, p portalapi.Principal, id uint64, request portalapi.BindingDraftRequest) (portalapi.BindingRevision, error) {
	return call[portalapi.BindingRevision](ctx, s.client, p, "updateBindingDraft", struct {
		RevisionID uint64                        `json:"revisionId"`
		Draft      portalapi.BindingDraftRequest `json:"draft"`
	}{id, request})
}
func (s *CapabilityService) TransitionBinding(ctx context.Context, p portalapi.Principal, id uint64, action string) (portalapi.BindingRevision, error) {
	return call[portalapi.BindingRevision](ctx, s.client, p, "transitionBinding", struct {
		RevisionID uint64 `json:"revisionId"`
		Action     string `json:"action"`
	}{id, action})
}
func (s *CapabilityService) Activate(ctx context.Context, p portalapi.Principal, request portalapi.ActivationRequest) (portalapi.PortalActivation, error) {
	return call[portalapi.PortalActivation](ctx, s.client, p, "activate", request)
}
func (s *CapabilityService) RollbackActivation(ctx context.Context, p portalapi.Principal, sourceID, expectedCurrentID uint64, reason string) (portalapi.PortalActivation, error) {
	return call[portalapi.PortalActivation](ctx, s.client, p, "rollbackActivation", struct {
		SourceID          uint64 `json:"sourceId"`
		ExpectedCurrentID uint64 `json:"expectedCurrentId"`
		Reason            string `json:"reason"`
	}{sourceID, expectedCurrentID, reason})
}
func (s *CapabilityService) ListActivations(ctx context.Context, p portalapi.Principal) ([]portalapi.PortalActivation, error) {
	return call[[]portalapi.PortalActivation](ctx, s.client, p, "listActivations", struct{}{})
}
func (s *CapabilityService) ListTestTargetBindings(ctx context.Context, p portalapi.Principal) ([]portalapi.TestTargetBinding, error) {
	return call[[]portalapi.TestTargetBinding](ctx, s.client, p, "listTestTargetBindings", struct{}{})
}
func (s *CapabilityService) PutTestTargetBinding(ctx context.Context, p portalapi.Principal, id string, request portalapi.PutTestTargetBindingRequest) (portalapi.TestTargetBinding, error) {
	return call[portalapi.TestTargetBinding](ctx, s.client, p, "putTestTargetBinding", struct {
		ID      string                                `json:"id"`
		Binding portalapi.PutTestTargetBindingRequest `json:"binding"`
	}{id, request})
}
func (s *CapabilityService) ListTestReleases(ctx context.Context, p portalapi.Principal) ([]portalapi.TestRelease, error) {
	return call[[]portalapi.TestRelease](ctx, s.client, p, "listTestReleases", struct{}{})
}
func (s *CapabilityService) CreateTestRelease(ctx context.Context, p portalapi.Principal, request portalapi.CreateTestReleaseRequest) (portalapi.TestRelease, error) {
	return call[portalapi.TestRelease](ctx, s.client, p, "createTestRelease", request)
}
func (s *CapabilityService) RollbackTestRelease(ctx context.Context, p portalapi.Principal, id uint64) (portalapi.TestRelease, error) {
	return call[portalapi.TestRelease](ctx, s.client, p, "rollbackTestRelease", struct {
		ID uint64 `json:"id"`
	}{id})
}

type revisionRequest struct {
	RevisionID uint64 `json:"revisionId"`
	portalapi.PublishRequest
}

func call[T any](ctx context.Context, client CapabilityClient, p portalapi.Principal, operation string, request any) (T, error) {
	var zero T
	payload, err := json.Marshal(request)
	if err != nil {
		return zero, err
	}
	raw, err := client.Call(ctx, p, operation, payload)
	if err != nil {
		return zero, err
	}
	var response T
	if err := json.Unmarshal(raw, &response); err != nil {
		return zero, fmt.Errorf("解析 Portal capability %s 响应: %w", operation, err)
	}
	return response, nil
}
