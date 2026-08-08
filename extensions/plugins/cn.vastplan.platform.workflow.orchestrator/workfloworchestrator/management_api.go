package workfloworchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var managementAPIOperations = []string{
	"apiGovern",
	"apiCatalog", "apiListDefinitions", "apiPublishDefinition", "apiListBindings", "apiBindDefinition",
	"apiListInstances", "apiListTasks", "apiCompleteTask", "apiCancelInstance",
}

func handleManagementAPI(ctx context.Context, host sdk.Host, call *contractv1.CallContext, repository Repository, service *Service, actor Actor, operation string, payload []byte) ([]byte, error) {
	invocation, err := managementInvocation(payload, operation)
	if err != nil {
		return nil, err
	}
	switch operation {
	case "apiGovern":
		var request struct {
			FeatureID      string          `json:"featureId"`
			PreparePayload json.RawMessage `json:"preparePayload"`
			IdempotencyKey string          `json:"idempotencyKey"`
		}
		if err := strictDecode(invocation.Body, &request); err != nil {
			return nil, ErrInvalidState
		}
		return govern(ctx, host, call, repository, service, actor, workflowv1.GovernedOperationRequest{
			ServiceID:      invocation.ManagementTarget.ServiceID,
			FeatureID:      request.FeatureID,
			PreparePayload: request.PreparePayload,
			IdempotencyKey: request.IdempotencyKey,
		})
	case "apiCatalog":
		return marshalResult(service.ListCatalog(ctx, repository, actor))
	case "apiListDefinitions":
		return marshalResult(service.ListDefinitions(ctx, repository, actor))
	case "apiPublishDefinition":
		var definition workflowv1.Definition
		if err := strictDecode(invocation.Body, &definition); err != nil {
			return nil, ErrInvalidState
		}
		return marshalResult(service.PublishDefinition(ctx, repository, actor, definition))
	case "apiListBindings":
		return marshalResult(service.ListBindings(ctx, repository, actor, invocation.ManagementTarget.ServiceID))
	case "apiBindDefinition":
		var request struct {
			FeatureID        string                   `json:"featureId"`
			Definition       workflowv1.DefinitionRef `json:"definition"`
			ExpectedRevision int64                    `json:"expectedRevision"`
		}
		if err := strictDecode(invocation.Body, &request); err != nil {
			return nil, ErrInvalidState
		}
		return marshalResult(service.BindDefinition(ctx, repository, actor, invocation.ManagementTarget.ServiceID, request.FeatureID, request.Definition, request.ExpectedRevision))
	case "apiListInstances":
		return marshalResult(service.ListInstances(ctx, repository, actor, invocation.ManagementTarget.ServiceID))
	case "apiListTasks":
		return marshalResult(service.ListTasksForService(ctx, repository, actor, invocation.ManagementTarget.ServiceID))
	case "apiCompleteTask":
		var request workflowv1.CompleteTaskRequest
		if err := strictDecode(invocation.Body, &request); err != nil {
			return nil, ErrInvalidState
		}
		if err := requireManagementRecordService(ctx, repository, request.TaskID, kindTask, invocation.ManagementTarget.ServiceID); err != nil {
			return nil, err
		}
		instance, err := service.CompleteTask(ctx, repository, actor, request)
		if err == nil {
			instance, err = driveActions(ctx, host, call, repository, service, instance)
		}
		return marshalResult(instance, err)
	case "apiCancelInstance":
		var request workflowv1.CancelRequest
		if err := strictDecode(invocation.Body, &request); err != nil {
			return nil, ErrInvalidState
		}
		if err := requireManagementRecordService(ctx, repository, instanceKey(request.InstanceID), kindInstance, invocation.ManagementTarget.ServiceID); err != nil {
			return nil, err
		}
		return marshalResult(service.Cancel(ctx, repository, actor, request))
	default:
		return nil, fmt.Errorf("unsupported workflow management operation %q", operation)
	}
}

func requireManagementRecordService(ctx context.Context, repository Repository, id string, kind recordKind, serviceID string) error {
	record, err := repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if record.Kind != kind || record.ServiceID != serviceID {
		return ErrNotFound
	}
	return nil
}

func managementInvocation(payload []byte, operation string) (apiv1.GatewayInvocation, error) {
	var invocation apiv1.GatewayInvocation
	if err := strictDecode(payload, &invocation); err != nil || apiv1.ValidateGatewayInvocation(invocation) != nil {
		return apiv1.GatewayInvocation{}, ErrInvalidState
	}
	method := "GET"
	if operation == "apiGovern" || operation == "apiPublishDefinition" {
		method = "POST"
	} else if operation == "apiBindDefinition" {
		method = "PUT"
	} else if operation == "apiCompleteTask" || operation == "apiCancelInstance" {
		method = "POST"
	}
	if invocation.Method != method || invocation.ManagementTarget == nil || invocation.ManagementTarget.ServiceID == "" || invocation.ManagementTarget.ActivationID == 0 || invocation.ManagementTarget.ActivationID != invocation.ManagementTarget.Generation {
		return apiv1.GatewayInvocation{}, ErrInvalidState
	}
	if method == "GET" && len(invocation.Body) != 0 && string(invocation.Body) != "{}" && string(invocation.Body) != "null" {
		return apiv1.GatewayInvocation{}, errors.New("workflow management GET body must be empty")
	}
	return invocation, nil
}
