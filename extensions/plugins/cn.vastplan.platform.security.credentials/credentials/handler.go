package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
	sharedstatesdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/sharedstate"
)

func (s *Service) Handler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, op string) (*contractv1.CallResult, []byte, error) {
	var result *contractv1.CallResult
	var raw []byte
	var handlerErr error
	err := s.withTenantState(ctx, host, call, func() error {
		result, raw, handlerErr = s.handleLoaded(ctx, host, call, payload, op)
		return handlerErr
	})
	if err != nil {
		return credentialDomainError(err)
	}
	return result, raw, handlerErr
}

func (s *Service) handleLoaded(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, op string) (*contractv1.CallResult, []byte, error) {
	var in struct {
		Name        string `json:"name"`
		Value       string `json:"value"`
		Prefix      string `json:"prefix"`
		StageID     string `json:"stageId"`
		Handle      string `json:"handle"`
		Purpose     string `json:"purpose"`
		Resource    string `json:"resource"`
		Authority   string `json:"authority"`
		CandidateID string `json:"candidateId"`
		BeforeID    uint64 `json:"beforeId"`
		Limit       int    `json:"limit"`
	}
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, nil, err
	}
	var out any
	var err error
	switch op {
	case "put":
		var record Record
		record, err = s.Put(ctx, call, in.Name, in.Value)
		out = metadata(record)
	case "describe":
		var record Record
		record, err = s.Describe(call, in.Name)
		out = metadata(record)
	case "list":
		var records []Record
		records, err = s.List(call, in.Prefix)
		out = make([]Metadata, 0, len(records))
		for _, record := range records {
			out = append(out.([]Metadata), metadata(record))
		}
	case "listManagedAudit":
		out, err = s.ListManagedAudit(call, in.BeforeID, in.Limit)
	case "rotate":
		var record Record
		record, err = s.Rotate(ctx, call, in.Name)
		out = metadata(record)
	case "revoke":
		var record Record
		record, err = s.Revoke(call, in.Name)
		out = metadata(record)
	case "stageManaged":
		secret := []byte(in.Value)
		defer func() {
			for index := range secret {
				secret[index] = 0
			}
		}()
		out, err = s.StageManaged(ctx, call, in.Purpose, in.Resource, secret)
	case "stageDelegated":
		secret := []byte(in.Value)
		defer zeroBytes(secret)
		out, err = s.StageDelegated(ctx, host, call, in.Authority, secret)
	case "activateManaged":
		out, err = s.ActivateManaged(call, in.StageID)
	case "abortManaged":
		out, err = s.AbortManaged(call, in.StageID)
	case "activateDelegated":
		out, err = s.ActivateDelegated(call, in.StageID, in.CandidateID)
	case "prepareDelegated":
		out, err = s.PrepareDelegated(call, in.StageID, in.CandidateID)
	case "abortDelegated":
		out, err = s.AbortDelegated(call, in.StageID, in.CandidateID)
	case "retireManaged":
		out, err = s.RetireManaged(call, in.Handle)
	default:
		err = fmt.Errorf("不支持的凭证操作 %q", op)
	}
	if err != nil {
		code := "platform.credentials.invalid"
		if errors.Is(err, os.ErrNotExist) {
			code = "platform.credentials.not_found"
		}
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func credentialDomainError(err error) (*contractv1.CallResult, []byte, error) {
	code := "platform.credentials.unavailable"
	retryable := errors.Is(err, errStateConflict)
	var stateError *sharedstatesdk.ServiceError
	if errors.As(err, &stateError) {
		retryable = stateError.Retryable
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error(), Retryable: retryable}}, nil, nil
}
func Descriptor() []byte {
	return []byte(`{"title":"凭证管理","subcommands":[
		{"name":"put","description":"以 Vault Transit 加密后保存凭证","paramsSchema":{"type":"object","properties":{"name":{"type":"string"},"value":{"type":"string"}},"required":["name","value"]}},
		{"name":"describe","description":"读取凭证元数据，不返回明文或密文","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出当前租户的凭证元数据","paramsSchema":{"type":"object","properties":{"prefix":{"type":"string"}}}},
		{"name":"listManagedAudit","description":"列出脱敏托管凭证生命周期审计和维护状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"beforeId":{"type":"integer","minimum":1},"limit":{"type":"integer","minimum":1,"maximum":200}}}},
		{"name":"rotate","description":"通过 Vault Transit rewrap 轮换凭证包裹密钥","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"revoke","description":"撤销凭证引用","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"stageManaged","description":"由业务插件创建不可读取的凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"purpose":{"type":"string"},"resource":{"type":"string"},"value":{"type":"string"}},"required":["purpose","resource","value"]}},
		{"name":"stageDelegated","description":"以可信宿主一次性配置授权创建目标插件凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"authority":{"type":"string"},"value":{"type":"string"}},"required":["authority","value"]}},
		{"name":"prepareDelegated","description":"使配置候选引用仅在候选 Deployment 窗口可用","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"},"candidateId":{"type":"string"}},"required":["stageId","candidateId"]}},
		{"name":"activateManaged","description":"由创建者激活托管凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"}},"required":["stageId"]}},
		{"name":"abortManaged","description":"由创建者终止托管凭证候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"}},"required":["stageId"]}},
		{"name":"activateDelegated","description":"由配置协调器激活已绑定候选的委托凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"},"candidateId":{"type":"string"}},"required":["stageId","candidateId"]}},
		{"name":"abortDelegated","description":"由配置协调器终止已绑定候选的委托凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"stageId":{"type":"string"},"candidateId":{"type":"string"}},"required":["stageId","candidateId"]}},
		{"name":"retireManaged","description":"由创建者退役不再使用的托管凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"handle":{"type":"string"}},"required":["handle"]}}
	]}`)
}

func MaterialLeaseDescriptor() []byte {
	return []byte(`{"title":"可信宿主凭证 Material Lease","subcommands":[
		{"name":"issue","description":"向可信宿主一次性公钥签发短期加密 material lease","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"ref":{"type":"object","additionalProperties":false,"properties":{"handle":{"type":"string"},"scope":{"const":"tenant"},"owner":{"type":"string"},"purpose":{"type":"string"},"version":{"type":"integer","minimum":1}},"required":["handle","scope","owner","purpose","version"]},"recipientPublicKey":{"type":"string"}},"required":["ref","recipientPublicKey"]}}
	]}`)
}

func Contribution(s *Service) sdk.Contribution {
	h := func(op string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return s.Handler(ctx, host, call, payload, op)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: map[string]sdk.Handler{"put": h("put"), "describe": h("describe"), "list": h("list"), "listManagedAudit": h("listManagedAudit"), "rotate": h("rotate"), "revoke": h("revoke"), "stageManaged": h("stageManaged"), "stageDelegated": h("stageDelegated"), "prepareDelegated": h("prepareDelegated"), "activateManaged": h("activateManaged"), "abortManaged": h("abortManaged"), "activateDelegated": h("activateDelegated"), "abortDelegated": h("abortDelegated"), "retireManaged": h("retireManaged")}}
}

func MaterialLeaseContribution(s *Service) sdk.Contribution {
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: MaterialLeaseCapability, Descriptor: MaterialLeaseDescriptor(), Handlers: map[string]sdk.Handler{"issue": func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		return s.MaterialLeaseHandler(ctx, host, call, payload, "issue")
	}}}
}
