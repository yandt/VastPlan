package apiexposure

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var operations = []string{
	"list", "createDraft", "updateDraft", "submit", "approve", "publish", "retire",
	"listDataPlanes", "createDataPlaneDraft", "submitDataPlane", "approveDataPlane", "publishDataPlane",
	"retireDataPlane",
	"registerEndpointLease", "renewEndpointLease", "revokeEndpointLease",
	"issueDataPlaneTicket", "consumeDataPlaneTicket", "listAudit",
	"apiList", "apiCreateDraft", "apiUpdateDraft", "apiSubmit", "apiApprove", "apiPublish", "apiRetire",
}

func Contribution(service *Service) sdk.Contribution {
	handlers := make(map[string]sdk.Handler, len(operations))
	for _, name := range operations {
		operation := name
		handlers[operation] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			var raw []byte
			var err error
			if runtimeOperation(operation) {
				caller, projectErr := projectRuntimeCaller(callCtx)
				if projectErr != nil {
					return nil, nil, projectErr
				}
				raw, err = service.handleRuntime(ctx, caller, operation, payload)
			} else {
				principal, projectErr := projectPrincipal(callCtx)
				if projectErr != nil {
					return nil, nil, projectErr
				}
				if operation == "issueDataPlaneTicket" {
					raw, err = service.issueAndInstallTicket(ctx, host, callCtx, principal, payload)
				} else {
					raw, err = service.handlePrincipal(ctx, principal, operation, payload)
				}
			}
			if err != nil {
				if errors.Is(err, ErrForbidden) || errors.Is(err, ErrSelfApproval) {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: errorcode.PermissionDenied, Message: err.Error()}}, nil, nil
				}
				code := "platform.api-exposure.invalid"
				if errors.Is(err, ErrNotFound) {
					code = "platform.api-exposure.not_found"
				}
				if errors.Is(err, ErrInvalidState) {
					code = "platform.api-exposure.state_conflict"
				}
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func (s *Service) issueAndInstallTicket(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, principal Principal, payload []byte) ([]byte, error) {
	var request TicketRequest
	if err := decode(payload, &request); err != nil {
		return nil, err
	}
	grant, installation, err := s.IssueTicketInstallation(ctx, principal, request)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(struct {
		Ticket string       `json:"ticket"`
		Claims TicketClaims `json:"claims"`
	}{Ticket: installation.Ticket, Claims: installation.Claims})
	if err != nil {
		s.CancelTicket(grant.Ticket)
		return nil, err
	}
	op := installation.Target.Operation
	result, _, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: installation.Target.Capability, Operation: &op}, callCtx, raw)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		s.CancelTicket(grant.Ticket)
		return nil, errors.New("Data Plane 拒绝安装一次性 Ticket")
	}
	return json.Marshal(grant)
}

func (s *Service) handlePrincipal(ctx context.Context, principal Principal, operation string, payload []byte) ([]byte, error) {
	var result any
	switch operation {
	case "list":
		value, err := s.List(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "createDraft":
		var request CreateDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "updateDraft":
		var request UpdateDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.UpdateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "submit", "approve", "publish":
		var request revisionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.Transition(ctx, principal, request.RevisionID, operation)
		if err != nil {
			return nil, err
		}
		result = value
	case "retire":
		var request struct {
			ExposureID string `json:"exposureId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if err := s.Retire(ctx, principal, request.ExposureID); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	case "listDataPlanes":
		value, err := s.ListDataPlanes(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "createDataPlaneDraft":
		var request CreateDataPlaneDraftRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDataPlaneDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "submitDataPlane", "approveDataPlane", "publishDataPlane":
		var request revisionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		action := strings.TrimSuffix(operation, "DataPlane")
		value, err := s.TransitionDataPlane(ctx, principal, request.RevisionID, action)
		if err != nil {
			return nil, err
		}
		result = value
	case "retireDataPlane":
		var request struct {
			ExposureID string `json:"exposureId"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if err := s.RetireDataPlane(ctx, principal, request.ExposureID); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	case "issueDataPlaneTicket":
		var request TicketRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.IssueTicket(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "listAudit":
		if err := require(principal, "platform.api-exposure.read"); err != nil {
			return nil, err
		}
		s.mu.Lock()
		items := make([]AuditEvent, 0)
		for _, item := range s.state.Audit {
			if item.TenantID == principal.TenantID {
				items = append(items, item)
			}
		}
		s.mu.Unlock()
		result = map[string]any{"items": items}
	case "apiList":
		if _, err := gatewayInvocation(payload, "GET"); err != nil {
			return nil, err
		}
		value, err := s.List(ctx, principal)
		if err != nil {
			return nil, err
		}
		result = map[string]any{"items": value}
	case "apiCreateDraft":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		var request CreateDraftRequest
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		value, err := s.CreateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiUpdateDraft":
		invocation, err := gatewayInvocation(payload, "PUT")
		if err != nil {
			return nil, err
		}
		var request UpdateDraftRequest
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		request.RevisionID, err = pathUint64(invocation, "revisionId")
		if err != nil {
			return nil, err
		}
		value, err := s.UpdateDraft(ctx, principal, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiSubmit", "apiApprove", "apiPublish":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		var request struct{}
		if err := decode(invocation.Body, &request); err != nil {
			return nil, err
		}
		revisionID, err := pathUint64(invocation, "revisionId")
		if err != nil {
			return nil, err
		}
		action := strings.TrimPrefix(operation, "api")
		action = strings.ToLower(action[:1]) + action[1:]
		value, err := s.Transition(ctx, principal, revisionID, action)
		if err != nil {
			return nil, err
		}
		result = value
	case "apiRetire":
		invocation, err := gatewayInvocation(payload, "POST")
		if err != nil {
			return nil, err
		}
		if err := s.Retire(ctx, principal, invocation.PathParams["exposureId"]); err != nil {
			return nil, err
		}
		result = map[string]bool{"retired": true}
	default:
		return nil, fmt.Errorf("不支持 API Exposure 用户操作 %q", operation)
	}
	return json.Marshal(result)
}

func (s *Service) handleRuntime(ctx context.Context, caller RuntimeCaller, operation string, payload []byte) ([]byte, error) {
	var result any
	switch operation {
	case "registerEndpointLease":
		var request EndpointLeaseRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.RegisterEndpointLease(ctx, caller, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "renewEndpointLease":
		var request EndpointLeaseRenewal
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.RenewEndpointLease(ctx, caller, request)
		if err != nil {
			return nil, err
		}
		result = value
	case "revokeEndpointLease":
		var request EndpointLeaseRevocation
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		if err := s.RevokeEndpointLease(ctx, caller, request); err != nil {
			return nil, err
		}
		result = map[string]bool{"revoked": true}
	case "consumeDataPlaneTicket":
		var request TicketConsumption
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		value, err := s.ConsumeTicket(ctx, caller, request)
		if err != nil {
			return nil, err
		}
		result = value
	default:
		return nil, fmt.Errorf("不支持 API Exposure Runtime 操作 %q", operation)
	}
	return json.Marshal(result)
}

type revisionRequest struct {
	RevisionID uint64 `json:"revisionId"`
}

func Descriptor() []byte {
	descriptions := map[string]string{
		"list": "列出 HTTP API Exposure revisions", "createDraft": "创建 HTTP API Exposure 草稿", "updateDraft": "CAS 更新 HTTP API Exposure 草稿", "submit": "提交 HTTP API Exposure 审批", "approve": "审批 HTTP API Exposure", "publish": "发布 HTTP API Exposure", "retire": "退役 HTTP API Exposure",
		"listDataPlanes": "列出 Data Plane Exposure revisions", "createDataPlaneDraft": "创建 Data Plane Exposure 草稿", "submitDataPlane": "提交 Data Plane Exposure 审批", "approveDataPlane": "审批 Data Plane Exposure", "publishDataPlane": "发布 Data Plane Exposure", "retireDataPlane": "退役 Data Plane Exposure",
		"registerEndpointLease": "登记短时 Data Plane Endpoint Lease", "renewEndpointLease": "续租 Data Plane Endpoint", "revokeEndpointLease": "撤销 Data Plane Endpoint Lease",
		"issueDataPlaneTicket": "签发一次性 Data Plane Ticket", "consumeDataPlaneTicket": "原子消费 Data Plane Ticket", "listAudit": "读取 API Exposure 审计",
		"apiList": "HTTP API 查询适配器", "apiCreateDraft": "HTTP API 创建草稿适配器", "apiUpdateDraft": "HTTP API 更新草稿适配器", "apiSubmit": "HTTP API 提交适配器", "apiApprove": "HTTP API 审批适配器", "apiPublish": "HTTP API 发布适配器", "apiRetire": "HTTP API 退役适配器",
	}
	subcommands := make([]map[string]string, 0, len(operations))
	for _, operation := range operations {
		subcommands = append(subcommands, map[string]string{"name": operation, "description": descriptions[operation]})
	}
	raw, _ := json.Marshal(map[string]any{"title": "API 暴露治理", "subcommands": subcommands})
	return raw
}

func runtimeOperation(operation string) bool {
	return strings.Contains(" registerEndpointLease renewEndpointLease revokeEndpointLease consumeDataPlaneTicket ", " "+operation+" ")
}

func projectPrincipal(callCtx *contractv1.CallContext) (Principal, error) {
	if callCtx == nil || callCtx.Principal == nil || callCtx.Principal.UserId == "" || callCtx.TenantId == "" {
		return Principal{}, errors.New("API Exposure 操作必须携带可信 Principal")
	}
	roles := append([]string(nil), callCtx.Principal.SystemRoles...)
	if callCtx.Principal.IsAdmin {
		roles = append(roles, "platform.api-exposure.read", "platform.api-exposure.edit", "platform.api-exposure.approve", "platform.api-exposure.publish")
	}
	return Principal{ID: callCtx.Principal.UserId, TenantID: callCtx.TenantId, Roles: roles}, nil
}

func projectRuntimeCaller(callCtx *contractv1.CallContext) (RuntimeCaller, error) {
	if callCtx == nil || callCtx.Caller == nil || callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN || callCtx.Caller.Id == "" || callCtx.TenantId == "" {
		return RuntimeCaller{}, errors.New("Endpoint Lease 只接受可信插件调用方")
	}
	return RuntimeCaller{PluginID: callCtx.Caller.Id, TenantID: callCtx.TenantId}, nil
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("API Exposure 请求无效: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return errors.New("API Exposure 请求只能包含一个 JSON 文档")
	}
	return nil
}

func gatewayInvocation(raw []byte, method string) (apiv1.GatewayInvocation, error) {
	var invocation apiv1.GatewayInvocation
	if err := decode(raw, &invocation); err != nil {
		return apiv1.GatewayInvocation{}, err
	}
	if err := apiv1.ValidateGatewayInvocation(invocation); err != nil || invocation.Method != method {
		return apiv1.GatewayInvocation{}, errors.New("Gateway Invocation 与 API route 不匹配")
	}
	return invocation, nil
}

func pathUint64(invocation apiv1.GatewayInvocation, name string) (uint64, error) {
	value, err := strconv.ParseUint(invocation.PathParams[name], 10, 64)
	if err != nil || value == 0 {
		return 0, fmt.Errorf("path 参数 %s 无效", name)
	}
	return value, nil
}
