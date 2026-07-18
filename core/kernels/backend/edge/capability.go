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
func (s *CapabilityService) Rollback(ctx context.Context, p portalapi.Principal, id uint64, request portalapi.PublishRequest) (portalapi.Revision, error) {
	return call[portalapi.Revision](ctx, s.client, p, "rollback", revisionRequest{RevisionID: id, PublishRequest: request})
}
func (s *CapabilityService) Audit(ctx context.Context, p portalapi.Principal, id uint64) ([]portalapi.AuditEvent, error) {
	return call[[]portalapi.AuditEvent](ctx, s.client, p, "audit", revisionRequest{RevisionID: id})
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
