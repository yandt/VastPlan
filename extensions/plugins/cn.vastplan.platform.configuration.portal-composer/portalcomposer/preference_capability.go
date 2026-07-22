package portalcomposer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/portalapi"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	preferenceConflictCode = "portal.preference.conflict"
	preferenceInvalidCode  = "portal.preference.invalid"
)

var errPreferenceInvalid = errors.New("PortalPreference 请求无效")

func (s *Service) HandlePreference(principal portalapi.Principal, operation string, payload []byte) ([]byte, error) {
	store, err := s.preferenceStoreForCall()
	if err != nil {
		return nil, err
	}
	var result portalapi.PortalPreference
	switch operation {
	case "get":
		var request portalapi.GetPortalPreferenceRequest
		if err := decodePreferenceRequest(payload, &request); err != nil {
			return nil, err
		}
		if err := portalapi.ValidatePortalPreferenceScope(request.Scope); err != nil {
			return nil, fmt.Errorf("%w: %v", errPreferenceInvalid, err)
		}
		result, err = store.Get(principal, request.Scope)
	case "put":
		var request portalapi.PutPortalPreferenceRequest
		if err := decodePreferenceRequest(payload, &request); err != nil {
			return nil, err
		}
		if err := portalapi.ValidatePortalPreferenceScope(request.Scope); err != nil {
			return nil, fmt.Errorf("%w: %v", errPreferenceInvalid, err)
		}
		if err := portalapi.ValidatePortalPreferenceValues(request.Values); err != nil {
			return nil, fmt.Errorf("%w: %v", errPreferenceInvalid, err)
		}
		result, err = store.Put(principal, request)
	default:
		return nil, fmt.Errorf("%w: 不支持操作 %q", errPreferenceInvalid, operation)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

func (s *Service) preferenceStoreForCall() (*preferenceStore, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.preferenceStore == nil {
		return nil, errors.New("PortalPreference 尚未配置状态文件")
	}
	return s.preferenceStore, nil
}

func PreferenceContribution(service *Service) sdk.Contribution {
	handlers := map[string]sdk.Handler{}
	for _, operation := range []string{"get", "put"} {
		op := operation
		handlers[op] = func(ctx context.Context, host sdk.Host, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			if !trustedPreferenceCaller(callCtx) {
				return preferenceError(errorcode.PermissionDenied, "PortalPreference 只接受可信 Portal BFF 的已认证用户调用"), nil, nil
			}
			if err := service.ensureConfigured(ctx, host, callCtx); err != nil {
				return nil, nil, err
			}
			principal, err := projectPrincipal(callCtx)
			if err != nil {
				return preferenceError(errorcode.PermissionDenied, err.Error()), nil, nil
			}
			raw, err := service.HandlePreference(principal, op, payload)
			switch {
			case errors.Is(err, portalapi.ErrPreferenceConflict):
				return preferenceError(preferenceConflictCode, err.Error()), nil, nil
			case errors.Is(err, errPreferenceInvalid):
				return preferenceError(preferenceInvalidCode, err.Error()), nil, nil
			case errors.Is(err, portalapi.ErrForbidden):
				return preferenceError(errorcode.PermissionDenied, err.Error()), nil, nil
			case err != nil:
				return nil, nil, err
			default:
				return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
			}
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: portalapi.PreferenceCapability, Descriptor: PreferenceDescriptor(), Handlers: handlers}
}

func PreferenceDescriptor() []byte {
	return []byte(`{"title":"Portal 用户偏好","subcommands":[{"name":"get","description":"读取当前主体的 Portal 偏好"},{"name":"put","description":"以 CAS 保存当前主体的 Portal 偏好"}]}`)
}

func trustedPreferenceCaller(callCtx *contractv1.CallContext) bool {
	return callCtx != nil && callCtx.GetCaller().GetKind() == contractv1.CallerKind_CALLER_KIND_USER && callCtx.GetScene() == "portal.bff"
}

func preferenceError(code, message string) *contractv1.CallResult {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: message}}
}

func decodePreferenceRequest(payload []byte, target any) error {
	if len(payload) == 0 || len(payload) > 256<<10 {
		return fmt.Errorf("%w: payload 大小无效", errPreferenceInvalid)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("%w: %v", errPreferenceInvalid, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: 包含多余 JSON", errPreferenceInvalid)
	}
	return nil
}
