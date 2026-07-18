package interaction

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	uiv1 "cdsoft.com.cn/VastPlan/schemas/ui/v1"
	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
	contractv1 "cdsoft.com.cn/VastPlan/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/shared/go/interactionapi"
)

func (s *Service) ensureConfigured(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext) error {
	s.mu.Lock()
	configured := s.stateFile != ""
	s.mu.Unlock()
	if configured {
		return nil
	}
	op := "get"
	payload, _ := json.Marshal(map[string]string{"key": StateFileConfigKey})
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.KernelService, Capability: "kernel.config.get", Operation: &op}, callCtx, payload)
	if err != nil {
		return fmt.Errorf("读取 Interaction Broker 部署配置: %w", err)
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return errors.New("未提供 Interaction Broker 部署配置")
	}
	var stateFile string
	if err := json.Unmarshal(raw, &stateFile); err != nil || strings.TrimSpace(stateFile) == "" {
		return fmt.Errorf("%s 必须是非空 JSON 字符串", StateFileConfigKey)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configure(stateFile)
}

// Contribution adapts the trusted CallContext into the least amount of
// identity the broker needs. The request payload never supplies an identity.
func Contribution(service *Service) sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"open", "list", "get", "present", "respond", "cancel"} {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if err := service.ensureConfigured(ctx, host, callCtx); err != nil {
				return nil, nil, err
			}
			raw, err := service.Handle(ctx, callCtx, op, payload)
			if err != nil {
				if errors.Is(err, ErrForbidden) {
					return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: errorcode.PermissionDenied, Message: err.Error()}}, nil, nil
				}
				return nil, nil, err
			}
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	return []byte(`{"title":"跨端交互协调","subcommands":[{"name":"open","description":"创建交互任务"},{"name":"list","description":"列出当前用户可处理的交互"},{"name":"get","description":"读取交互任务状态"},{"name":"present","description":"标记交互已呈现"},{"name":"respond","description":"提交一次性终态响应"},{"name":"cancel","description":"取消来源侧交互任务"}]}`)
}

func (s *Service) Handle(ctx context.Context, callCtx *contractv1.CallContext, operation string, payload []byte) ([]byte, error) {
	switch operation {
	case "open":
		source, err := sourceSubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request uiv1.InteractionRequest
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.Open(ctx, source, request))
	case "list":
		subject, err := rendererSubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request struct {
			Surface uiv1.InteractionSurface `json:"surface"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.List(ctx, subject, request.Surface))
	case "get":
		subject, err := anySubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request struct {
			ID string `json:"id"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.Get(ctx, subject, request.ID))
	case "present":
		subject, err := rendererSubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request struct {
			ID      string                  `json:"id"`
			Surface uiv1.InteractionSurface `json:"surface"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.Present(ctx, subject, request.ID, request.Surface))
	case "respond":
		subject, err := rendererSubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request struct {
			ID       string                   `json:"id"`
			Surface  uiv1.InteractionSurface  `json:"surface"`
			Response uiv1.InteractionResponse `json:"response"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.Respond(ctx, subject, request.ID, request.Surface, request.Response))
	case "cancel":
		source, err := sourceSubject(callCtx)
		if err != nil {
			return nil, err
		}
		var request struct {
			ID string `json:"id"`
		}
		if err := decode(payload, &request); err != nil {
			return nil, err
		}
		return marshal(s.Cancel(ctx, source, request.ID))
	default:
		return nil, fmt.Errorf("不支持 Interaction Broker 操作 %q", operation)
	}
}

func decode(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("Interaction Broker 请求无效: %w", err)
	}
	return nil
}

func marshal[T any](value T, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

func sourceSubject(callCtx *contractv1.CallContext) (interactionapi.Subject, error) {
	subject, err := anySubject(callCtx)
	if err != nil {
		return interactionapi.Subject{}, err
	}
	if callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_PLUGIN && callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_RUNNER && callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_SYSTEM {
		return interactionapi.Subject{}, ErrForbidden
	}
	return subject, nil
}

func rendererSubject(callCtx *contractv1.CallContext) (interactionapi.Subject, error) {
	subject, err := anySubject(callCtx)
	if err != nil {
		return interactionapi.Subject{}, err
	}
	if callCtx.Caller.Kind != contractv1.CallerKind_CALLER_KIND_USER {
		return interactionapi.Subject{}, ErrForbidden
	}
	return subject, nil
}

func anySubject(callCtx *contractv1.CallContext) (interactionapi.Subject, error) {
	if callCtx == nil || callCtx.Caller == nil || strings.TrimSpace(callCtx.Caller.Id) == "" || strings.TrimSpace(callCtx.TenantId) == "" {
		return interactionapi.Subject{}, ErrForbidden
	}
	subject := interactionapi.Subject{ID: callCtx.Caller.Id, TenantID: callCtx.TenantId, System: callCtx.Caller.Kind == contractv1.CallerKind_CALLER_KIND_SYSTEM}
	if callCtx.Principal != nil {
		subject.Roles = append([]string(nil), callCtx.Principal.SystemRoles...)
		if callCtx.Principal.IsAdmin {
			subject.Roles = append(subject.Roles, "admin")
		}
	}
	return subject, nil
}
