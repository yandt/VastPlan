package workfloworchestrator

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/errorcode"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	workflowv1 "cdsoft.com.cn/VastPlan/contracts/schemas/workflow/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var operations = append([]string{"registerCatalog", "govern", "publishDefinition", "bindDefinition", "getInstance", "listTasks", "completeTask", "cancel"}, managementAPIOperations...)

func Contribution(service *Service) sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range operations {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			repository, err := NewRecordRepository(host, call)
			if err != nil {
				return nil, nil, err
			}
			result, err := handle(ctx, host, call, repository, service, op, payload)
			if err != nil {
				if errors.Is(err, ErrForbidden) {
					return failure(errorcode.PermissionDenied, err), nil, nil
				}
				if errors.Is(err, ErrConflict) || errors.Is(err, ErrInvalidState) {
					return failure("workflow.conflict", err), nil, nil
				}
				if errors.Is(err, ErrNotFound) {
					return failure("workflow.not_found", err), nil, nil
				}
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, result, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	return []byte(`{"title":"通用流程编排","subcommands":[{"name":"registerCatalog","description":"从可信贡献索引装配流程功能点与节点目录","audience":"workload"},{"name":"govern","description":"按签名准备入口冻结资源并透明选择流程或直通","audience":"user"},{"name":"publishDefinition","description":"发布不可变流程定义修订","audience":"user"},{"name":"bindDefinition","description":"绑定服务功能点与精确定义修订","audience":"user"},{"name":"getInstance","description":"读取流程实例","audience":"user"},{"name":"listTasks","description":"读取当前主体可处理任务","audience":"user"},{"name":"completeTask","description":"以 CAS 完成人工任务","audience":"user"},{"name":"cancel","description":"以 CAS 取消未完成流程实例","audience":"user"},{"name":"apiGovern","description":"管理 API 提交受治理操作","audience":"user"},{"name":"apiCatalog","description":"管理 API 读取流程功能点与节点目录","audience":"user"},{"name":"apiListDefinitions","description":"管理 API 列出流程定义","audience":"user"},{"name":"apiPublishDefinition","description":"管理 API 发布流程定义","audience":"user"},{"name":"apiListBindings","description":"管理 API 列出服务绑定","audience":"user"},{"name":"apiBindDefinition","description":"管理 API 以 CAS 保存服务绑定","audience":"user"},{"name":"apiListInstances","description":"管理 API 列出流程实例","audience":"user"},{"name":"apiListTasks","description":"管理 API 列出当前主体任务","audience":"user"},{"name":"apiCompleteTask","description":"管理 API 完成人工任务","audience":"user"},{"name":"apiCancelInstance","description":"管理 API 取消流程实例","audience":"user"}]}`)
}

func handle(ctx context.Context, host sdk.Host, call *contractv1.CallContext, repository Repository, service *Service, operation string, payload []byte) ([]byte, error) {
	actor, err := actorFromCall(call)
	if err != nil {
		return nil, err
	}
	if slices.Contains(managementAPIOperations, operation) {
		return handleManagementAPI(ctx, host, call, repository, service, actor, operation, payload)
	}
	switch operation {
	case "registerCatalog":
		var index pluginv1.ContributionIndexSnapshot
		if err := decodeRequest(payload, &index); err != nil {
			return nil, err
		}
		return marshalResult(service.RegisterCatalog(ctx, repository, actor, index))
	case "govern":
		var request workflowv1.GovernedOperationRequest
		if err := decodeRequest(payload, &request); err != nil {
			return nil, err
		}
		return govern(ctx, host, call, repository, service, actor, request)
	case "publishDefinition":
		var definition workflowv1.Definition
		if err := decodeRequest(payload, &definition); err != nil {
			return nil, err
		}
		return marshalResult(service.PublishDefinition(ctx, repository, actor, definition))
	case "bindDefinition":
		var request struct {
			ServiceID        string                   `json:"serviceId"`
			FeatureID        string                   `json:"featureId"`
			Definition       workflowv1.DefinitionRef `json:"definition"`
			ExpectedRevision int64                    `json:"expectedRevision"`
		}
		if err := decodeRequest(payload, &request); err != nil {
			return nil, err
		}
		return marshalResult(service.BindDefinition(ctx, repository, actor, request.ServiceID, request.FeatureID, request.Definition, request.ExpectedRevision))
	case "getInstance":
		var request struct {
			ID string `json:"id"`
		}
		if err := decodeRequest(payload, &request); err != nil {
			return nil, err
		}
		return marshalResult(service.GetInstance(ctx, repository, request.ID))
	case "listTasks":
		if len(bytes.TrimSpace(payload)) > 0 && string(bytes.TrimSpace(payload)) != "{}" {
			return nil, ErrInvalidState
		}
		return marshalResult(service.ListTasks(ctx, repository, actor))
	case "completeTask":
		var request workflowv1.CompleteTaskRequest
		if err := decodeRequest(payload, &request); err != nil {
			return nil, err
		}
		instance, err := service.CompleteTask(ctx, repository, actor, request)
		if err == nil {
			instance, err = driveActions(ctx, host, call, repository, service, instance)
		}
		return marshalResult(instance, err)
	case "cancel":
		var request workflowv1.CancelRequest
		if err := decodeRequest(payload, &request); err != nil {
			return nil, err
		}
		return marshalResult(service.Cancel(ctx, repository, actor, request))
	default:
		return nil, fmt.Errorf("unsupported workflow operation %q", operation)
	}
}

func govern(ctx context.Context, host sdk.Host, call *contractv1.CallContext, repository Repository, service *Service, actor Actor, request workflowv1.GovernedOperationRequest) ([]byte, error) {
	feature, err := service.feature(ctx, repository, request.FeatureID)
	if err != nil {
		return nil, err
	}
	if feature.Descriptor.Prepare == nil || strings.TrimSpace(request.ServiceID) == "" || strings.TrimSpace(request.IdempotencyKey) == "" || len(request.PreparePayload) == 0 {
		return nil, ErrInvalidState
	}
	prepare := feature.Descriptor.Prepare
	operation := prepare.Operation
	result, payload, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: prepare.Capability, Operation: &operation}, call, request.PreparePayload)
	if err != nil {
		return nil, err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return nil, fmt.Errorf("%w: governed resource preparation failed", ErrInvalidState)
	}
	var prepared workflowv1.PreparedResource
	if err := strictDecode(payload, &prepared); err != nil {
		return nil, fmt.Errorf("%w: invalid prepared resource", ErrInvalidState)
	}
	if err := workflowv1.ValidatePreparedResource(prepared, feature.Descriptor); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidState, err)
	}
	identity := sha256.Sum256([]byte(request.ServiceID + "\x00" + request.FeatureID + "\x00" + prepared.Resource.Kind + "\x00" + prepared.Resource.ID + "\x00" + fmt.Sprint(prepared.Revision)))
	instance, err := service.Start(ctx, repository, actor, workflowv1.StartRequest{
		ID:             fmt.Sprintf("governed-%x", identity[:16]),
		ServiceID:      request.ServiceID,
		FeatureID:      request.FeatureID,
		Resource:       prepared.Resource,
		ResourceDigest: prepared.Digest,
		IdempotencyKey: request.IdempotencyKey,
	})
	if err == nil {
		instance, err = driveActions(ctx, host, call, repository, service, instance)
	}
	return marshalResult(workflowv1.GovernedOperationResult{Resource: prepared, Instance: instance}, err)
}

func driveActions(ctx context.Context, host sdk.Host, call *contractv1.CallContext, repository Repository, service *Service, instance workflowv1.Instance) (workflowv1.Instance, error) {
	for instance.Status == workflowv1.InstanceRunning {
		work, action, request, err := service.PendingAction(ctx, repository, instance.ID)
		if errors.Is(err, ErrNotFound) {
			return instance, nil
		}
		if err != nil {
			return instance, err
		}
		raw, _ := json.Marshal(request)
		operation := action.Operation
		response, payload, callErr := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: action.Capability, Operation: &operation}, call, raw)
		if callErr != nil {
			return instance, callErr
		}
		if response == nil || response.Status != contractv1.CallResult_STATUS_OK {
			if response == nil || response.Error == nil || response.Error.Retryable {
				return instance, errors.New("workflow action is temporarily unavailable")
			}
			return service.CompleteAction(ctx, repository, workflowv1.CompleteActionRequest{WorkID: work.ID, ExpectedRevision: work.Revision, Succeeded: false, ErrorCode: response.Error.Code})
		}
		instance, err = service.CompleteAction(ctx, repository, workflowv1.CompleteActionRequest{WorkID: work.ID, ExpectedRevision: work.Revision, Succeeded: true, Result: payload})
		if err != nil {
			return instance, err
		}
	}
	return instance, nil
}

func actorFromCall(call *contractv1.CallContext) (Actor, error) {
	if call == nil || call.Caller == nil || strings.TrimSpace(call.Caller.Id) == "" || strings.TrimSpace(call.TenantId) == "" {
		return Actor{}, ErrForbidden
	}
	actor := Actor{ID: call.Caller.Id, System: call.Caller.Kind == contractv1.CallerKind_CALLER_KIND_SYSTEM}
	if call.Principal != nil {
		if call.Caller.Kind == contractv1.CallerKind_CALLER_KIND_PLUGIN && strings.TrimSpace(call.Principal.UserId) != "" {
			actor.ID = call.Principal.UserId
		}
		actor.Roles = append([]string(nil), call.Principal.SystemRoles...)
	}
	return actor, nil
}

func decodeRequest(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid workflow request: %w", err)
	}
	return nil
}

func marshalResult[T any](value T, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func failure(code string, err error) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}
}
