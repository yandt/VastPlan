package pluginsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/pluginconfiguration"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

type operationRequest struct {
	ID               string            `json:"id"`
	ConfigurationID  string            `json:"configurationId"`
	CatalogDigest    string            `json:"catalogDigest"`
	Values           json.RawMessage   `json:"values"`
	Secrets          map[string]string `json:"secrets"`
	ExpectedRevision uint64            `json:"expectedRevision"`
}

func ensureEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return ErrInvalid
	}
	return nil
}

func decodeStrict(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureEOF(decoder)
}

func (s *Service) Handler(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte, operation string) (*contractv1.CallResult, []byte, error) {
	if err := s.ensureConfigured(ctx, host, call); err != nil {
		return domainError("platform.plugin_configuration.unavailable", err)
	}
	s.workflowMu.Lock()
	defer s.workflowMu.Unlock()
	if err := s.recoverInterrupted(ctx, host, call); err != nil {
		return domainError("platform.plugin_configuration.unavailable", err)
	}
	var request operationRequest
	if err := decodeStrict(payload, &request); err != nil {
		return domainError("platform.plugin_configuration.invalid", ErrInvalid)
	}
	out, err := s.execute(ctx, host, call, operation, request)
	if err != nil {
		return domainError(domainErrorCode(err), err)
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}

func (s *Service) execute(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, request operationRequest) (any, error) {
	tenant, _, tenantErr := tenantAndActor(call)
	if tenantErr != nil {
		return nil, tenantErr
	}
	switch operation {
	case "listDefinitions":
		catalogs, err := s.catalogs(ctx, host, call)
		return map[string]any{"items": s.publicDefinitions(tenant, catalogs)}, err
	case "getDefinition":
		catalogs, err := s.catalogs(ctx, host, call)
		if err != nil {
			return nil, err
		}
		view, err := findDefinition(catalogs, request.ConfigurationID, request.CatalogDigest)
		if err != nil {
			return nil, err
		}
		return s.publicDefinition(tenant, view), nil
	case "listCandidates":
		items, err := s.ListCandidates(call)
		return map[string]any{"items": items}, err
	case "createDraft":
		return s.CreateDraft(ctx, host, call, pluginconfiguration.CreateDraftRequest{
			ConfigurationID: request.ConfigurationID,
			CatalogDigest:   request.CatalogDigest,
			Values:          request.Values,
			Secrets:         request.Secrets,
		})
	case "discardDraft":
		return s.DiscardDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitDraft":
		return s.SubmitDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "activateCandidate":
		return s.ActivateCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitProfileDraft":
		return s.SubmitProfileDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveProfileCandidate":
		return s.ApproveProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "activateProfileCandidate":
		return s.ActivateProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortProfileCandidate":
		return s.AbortProfileCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "submitHotServiceDraft":
		return s.SubmitHotServiceDraft(ctx, host, call, request.ID, request.ExpectedRevision)
	case "approveHotServiceCandidate":
		return s.ApproveHotServiceCandidate(call, request.ID, request.ExpectedRevision)
	case "activateHotServiceCandidate":
		return s.ActivateHotServiceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	case "abortHotServiceCandidate":
		return s.AbortHotServiceCandidate(ctx, host, call, request.ID, request.ExpectedRevision)
	default:
		return nil, ErrInvalid
	}
}

func domainErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrNotFound):
		return "platform.plugin_configuration.not_found"
	case errors.Is(err, ErrConflict):
		return "platform.plugin_configuration.conflict"
	case strings.Contains(err.Error(), "不可用"):
		return "platform.plugin_configuration.unavailable"
	default:
		return "platform.plugin_configuration.invalid"
	}
}

func domainError(code string, err error) (*contractv1.CallResult, []byte, error) {
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_ERROR, Error: &contractv1.Error{Code: code, Message: err.Error()}}, nil, nil
}

func Descriptor() []byte {
	return []byte(`{"title":"插件配置协调器","subcommands":[
		{"name":"listDefinitions","description":"列出活动部署中的可信插件配置定义","paramsSchema":{"type":"object","additionalProperties":false,"properties":{}}},
		{"name":"getDefinition","description":"按不透明资源 ID 读取可信配置定义","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"catalogDigest":{"type":"string"}},"required":["configurationId"]}},
		{"name":"listCandidates","description":"列出配置候选与生效状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{}}},
		{"name":"createDraft","description":"按活动目录和签名 Schema 创建配置草稿并委托暂存只写秘密","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"catalogDigest":{"type":"string"},"values":{"type":"object"},"secrets":{"type":"object","additionalProperties":{"type":"string"}}},"required":["configurationId","catalogDigest","values"]}},
		{"name":"discardDraft","description":"以 CAS 放弃尚未发布的配置草稿","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"submitDraft","description":"把 Application Deployment 配置草稿提交为受治理服务修订","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"activateCandidate","description":"发布已审批配置修订并以 readiness 驱动凭证提交或回滚","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"submitProfileDraft","description":"把 Platform Profile 配置草稿提交为独立候选和异人审批","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"approveProfileCandidate","description":"由不同主体批准 Platform Profile 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"activateProfileCandidate","description":"执行 Platform Catalog、Deployment、readiness 与凭证激活 Saga","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"abortProfileCandidate","description":"放弃待审批或已审批的 Platform Profile 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"submitHotServiceDraft","description":"向目标插件 configuration.v1 控制器准备 Hot Service 候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"approveHotServiceCandidate","description":"由不同主体批准 Hot Service 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"activateHotServiceCandidate","description":"激活候选凭证并原子提交目标插件 Hot Service 配置","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
		,{"name":"abortHotServiceCandidate","description":"放弃尚未提交的 Hot Service 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
	]}`)
}

func Contribution(service *Service) sdk.Contribution {
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, call, payload, operation)
		}
	}
	operations := []string{"listDefinitions", "getDefinition", "listCandidates", "createDraft", "discardDraft", "submitDraft", "activateCandidate", "submitProfileDraft", "approveProfileCandidate", "activateProfileCandidate", "abortProfileCandidate", "submitHotServiceDraft", "approveHotServiceCandidate", "activateHotServiceCandidate", "abortHotServiceCandidate"}
	handlers := make(map[string]sdk.Handler, len(operations))
	for _, operation := range operations {
		handlers[operation] = handler(operation)
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}
