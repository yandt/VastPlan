package edge

import (
	"context"
	"encoding/json"
	"fmt"

	uiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/ui/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/interactionapi"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
)

// InteractionService is the browser-facing port. It deliberately receives the
// verified Edge principal rather than any identity claimed by browser JSON.
type InteractionService interface {
	List(context.Context, portalapi.Principal) ([]interactionapi.Record, error)
	Get(context.Context, portalapi.Principal, string) (interactionapi.Record, error)
	Present(context.Context, portalapi.Principal, string) (interactionapi.Record, error)
	Respond(context.Context, portalapi.Principal, string, uiv1.InteractionResponse) (interactionapi.Record, error)
}

type CapabilityInteractionService struct{ client CapabilityClient }

func NewCapabilityInteractionService(client CapabilityClient) (*CapabilityInteractionService, error) {
	if client == nil {
		return nil, fmt.Errorf("Interaction capability client 不能为空")
	}
	return &CapabilityInteractionService{client: client}, nil
}

func (s *CapabilityInteractionService) List(ctx context.Context, principal portalapi.Principal) ([]interactionapi.Record, error) {
	return interactionCall[[]interactionapi.Record](ctx, s.client, principal, "list", struct {
		Surface uiv1.InteractionSurface `json:"surface"`
	}{Surface: uiv1.SurfaceFrontend})
}

func (s *CapabilityInteractionService) Get(ctx context.Context, principal portalapi.Principal, id string) (interactionapi.Record, error) {
	return interactionCall[interactionapi.Record](ctx, s.client, principal, "get", struct {
		ID string `json:"id"`
	}{ID: id})
}

func (s *CapabilityInteractionService) Present(ctx context.Context, principal portalapi.Principal, id string) (interactionapi.Record, error) {
	return interactionCall[interactionapi.Record](ctx, s.client, principal, "present", struct {
		ID      string                  `json:"id"`
		Surface uiv1.InteractionSurface `json:"surface"`
	}{ID: id, Surface: uiv1.SurfaceFrontend})
}

func (s *CapabilityInteractionService) Respond(ctx context.Context, principal portalapi.Principal, id string, response uiv1.InteractionResponse) (interactionapi.Record, error) {
	return interactionCall[interactionapi.Record](ctx, s.client, principal, "respond", struct {
		ID       string                   `json:"id"`
		Surface  uiv1.InteractionSurface  `json:"surface"`
		Response uiv1.InteractionResponse `json:"response"`
	}{ID: id, Surface: uiv1.SurfaceFrontend, Response: response})
}

func interactionCall[T any](ctx context.Context, client CapabilityClient, principal portalapi.Principal, operation string, request any) (T, error) {
	var zero T
	payload, err := json.Marshal(request)
	if err != nil {
		return zero, err
	}
	raw, err := client.Call(ctx, principal, operation, payload)
	if err != nil {
		return zero, err
	}
	var result T
	if err := json.Unmarshal(raw, &result); err != nil {
		return zero, fmt.Errorf("解析 Interaction capability %s 响应: %w", operation, err)
	}
	return result, nil
}
