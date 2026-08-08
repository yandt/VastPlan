// Package navigationorganizer exposes the service-scoped Portal navigation management adapter.
package navigationorganizer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	frontendcompositionv1 "cdsoft.com.cn/VastPlan/contracts/schemas/composition/frontend/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	PluginID      = portalapi.NavigationOrganizerPluginID
	PluginVersion = "0.1.0"
	Capability    = "portal.navigation"
)

type publishRequest struct {
	CandidateID          string             `json:"candidateId"`
	ExpectedActivationID uint64             `json:"expectedActivationId"`
	Folders              []navigationFolder `json:"folders"`
}

type navigationFolder struct {
	ID      string                                      `json:"id"`
	Label   string                                      `json:"label"`
	Labels  map[string]string                           `json:"labels,omitempty"`
	Icon    *frontendcompositionv1.NavigationFolderIcon `json:"icon,omitempty"`
	Members []string                                    `json:"members"`
	Order   *int                                        `json:"order,omitempty"`
}

func Contribution() sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor(), Handlers: map[string]sdk.Handler{
		"apiRead": func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			invocation, err := gatewayInvocation(payload, "GET")
			if err != nil {
				return organizerError("portal.navigation.invalid", err.Error(), false), nil, nil
			}
			return read(ctx, host, call, invocation)
		},
		"apiPublish": func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			invocation, err := gatewayInvocation(payload, "PUT")
			if err != nil {
				return organizerError("portal.navigation.invalid", err.Error(), false), nil, nil
			}
			return publish(ctx, host, call, invocation)
		},
	}}
}

func read(ctx context.Context, host sdk.Host, call *contractv1.CallContext, invocation apiv1.GatewayInvocation) (*contractv1.CallResult, []byte, error) {
	target, err := managementTarget(invocation)
	if err != nil {
		return organizerError("portal.navigation.invalid", err.Error(), false), nil, nil
	}
	lookup := portalapi.NavigationConfigurationLookup{PortalID: target.PortalID, ServiceID: target.ServiceID}
	var snapshot portalapi.NavigationConfigurationSnapshot
	if err := callComposer(ctx, host, call, portalapi.ReadNavigationConfigurationOperation, lookup, &snapshot); err != nil {
		return mapComposerError(err), nil, nil
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func publish(ctx context.Context, host sdk.Host, call *contractv1.CallContext, invocation apiv1.GatewayInvocation) (*contractv1.CallResult, []byte, error) {
	target, err := managementTarget(invocation)
	if err != nil {
		return organizerError("portal.navigation.invalid", err.Error(), false), nil, nil
	}
	var input publishRequest
	if err := decode(invocation.Body, &input); err != nil || input.CandidateID == "" {
		return organizerError("portal.navigation.invalid", "Navigation publication request is invalid", false), nil, nil
	}
	if input.ExpectedActivationID != target.ActivationID {
		return organizerError("portal.navigation.conflict", "Portal Generation has changed", true), nil, nil
	}
	folders := make([]frontendcompositionv1.NavigationFolder, len(input.Folders))
	for index, folder := range input.Folders {
		folders[index] = frontendcompositionv1.NavigationFolder{
			ID: folder.ID, ServiceID: target.ServiceID, Label: folder.Label, Labels: folder.Labels,
			Icon: folder.Icon, Members: folder.Members, Order: folder.Order,
		}
	}
	request := portalapi.NavigationConfigurationRequest{
		CandidateID: input.CandidateID, PortalID: target.PortalID, ServiceID: target.ServiceID,
		ExpectedActivationID: target.ActivationID, Folders: folders,
	}
	var prepared portalapi.NavigationConfigurationPreparation
	if err := callComposer(ctx, host, call, portalapi.PrepareNavigationConfigurationOperation, request, &prepared); err != nil {
		return mapComposerError(err), nil, nil
	}
	lookup := portalapi.NavigationConfigurationLookup{CandidateID: input.CandidateID, PortalID: target.PortalID, ServiceID: target.ServiceID}
	if prepared.Status != portalapi.NavigationConfigurationCommitted {
		if prepared.Status != portalapi.NavigationConfigurationPrepared {
			return organizerError("portal.navigation.conflict", "Navigation candidate was not prepared", true), nil, nil
		}
		if err := callComposer(ctx, host, call, portalapi.CommitNavigationConfigurationOperation, lookup, &prepared); err != nil {
			_ = callComposer(ctx, host, call, portalapi.AbortNavigationConfigurationOperation, lookup, nil)
			return mapComposerError(err), nil, nil
		}
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func managementTarget(invocation apiv1.GatewayInvocation) (apiv1.ManagementInvocationTarget, error) {
	if invocation.ManagementTarget == nil || invocation.ManagementTarget.PortalID == "" || invocation.ManagementTarget.ServiceID == "" || invocation.ManagementTarget.ActivationID == 0 || invocation.ManagementTarget.ActivationID != invocation.ManagementTarget.Generation {
		return apiv1.ManagementInvocationTarget{}, errors.New("trusted management target is missing")
	}
	return *invocation.ManagementTarget, nil
}

type composerCallError struct {
	code      string
	message   string
	retryable bool
}

func (e *composerCallError) Error() string { return e.code + ": " + e.message }

func callComposer(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input, output any) error {
	if host == nil || call == nil {
		return errors.New("Portal Composer call context is incomplete")
	}
	raw, err := json.Marshal(input)
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
			return &composerCallError{code: result.Error.Code, message: result.Error.Message, retryable: result.Error.Retryable}
		}
		return errors.New("Portal Composer returned a non-success status")
	}
	if output != nil {
		return decode(response, output)
	}
	return nil
}

func gatewayInvocation(raw []byte, method string) (apiv1.GatewayInvocation, error) {
	var invocation apiv1.GatewayInvocation
	if err := decode(raw, &invocation); err != nil {
		return apiv1.GatewayInvocation{}, err
	}
	if err := apiv1.ValidateGatewayInvocation(invocation); err != nil || invocation.Method != method {
		return apiv1.GatewayInvocation{}, errors.New("Gateway Invocation does not match the API route")
	}
	return invocation, nil
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("request must contain one JSON document")
	}
	return nil
}

func mapComposerError(err error) *contractv1.CallResult {
	var callError *composerCallError
	if errors.As(err, &callError) {
		switch callError.code {
		case "portal.composer.conflict":
			return organizerError("portal.navigation.conflict", callError.message, true)
		case "portal.catalog.rejected":
			return organizerError("portal.navigation.invalid", callError.message, false)
		case errorcode.PermissionDenied:
			return organizerError(errorcode.PermissionDenied, callError.message, false)
		default:
			return organizerError("portal.navigation.unavailable", callError.message, callError.retryable)
		}
	}
	return organizerError("portal.navigation.unavailable", err.Error(), true)
}

func organizerError(code, message string, retryable bool) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message, Retryable: retryable}}
}

func descriptor() []byte {
	return []byte(`{"title":"Portal navigation organizer","subcommands":[{"name":"apiRead","description":"Read service navigation folders"},{"name":"apiPublish","description":"Publish service navigation folders"}]}`)
}
