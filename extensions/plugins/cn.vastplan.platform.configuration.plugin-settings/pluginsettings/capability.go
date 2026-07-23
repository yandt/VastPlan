package pluginsettings

import (
	"context"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var capabilityOperations = []string{
	"listDefinitions", "getDefinition", "listCandidates", "createDraft", "discardDraft", "submitDraft", "activateCandidate",
	"submitProfileDraft", "approveProfileCandidate", "activateProfileCandidate", "abortProfileCandidate",
	"submitHotServiceDraft", "approveHotServiceCandidate", "activateHotServiceCandidate", "abortHotServiceCandidate",
	"submitScopedDraft", "approveScopedCandidate", "activateScopedCandidate", "abortScopedCandidate",
	"listResourceItems", "getResourceItem", "createResourceDraft", "updateResourceDraft", "deleteResourceDraft",
	"submitResourceDraft", "approveResourceCandidate", "activateResourceCandidate", "abortResourceCandidate",
}

func Contribution(service *Service) sdk.Contribution {
	handlers := make(map[string]sdk.Handler, len(capabilityOperations))
	for _, operation := range capabilityOperations {
		operation := operation
		handlers[operation] = func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.Handler(ctx, host, call, payload, operation)
		}
	}
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: Descriptor(), Handlers: handlers}
}

func Descriptor() []byte {
	return []byte(`{"title":"插件配置协调器","subcommands":[
		{"name":"listDefinitions","description":"列出活动部署中的可信插件配置定义","paramsSchema":{"type":"object","additionalProperties":false,"properties":{}}},
		{"name":"getDefinition","description":"按不透明资源 ID 读取可信配置定义","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"catalogDigest":{"type":"string"},"scopeSubjectId":{"type":"string","maxLength":256}},"required":["configurationId"]}},
		{"name":"listCandidates","description":"列出配置候选与生效状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{}}},
		{"name":"createDraft","description":"按活动目录和签名 Schema 创建配置草稿并委托暂存只写秘密","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"catalogDigest":{"type":"string"},"scopeSubjectId":{"type":"string","maxLength":256},"values":{"type":"object"},"secrets":{"type":"object","additionalProperties":{"type":"string"}}},"required":["configurationId","catalogDigest","values"]}},
		{"name":"discardDraft","description":"以 CAS 放弃尚未发布的配置草稿","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"submitDraft","description":"把 Application Deployment 配置草稿提交为受治理服务修订","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"activateCandidate","description":"发布已审批配置修订并以 readiness 驱动凭证提交或回滚","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"submitProfileDraft","description":"把 Platform Profile 配置草稿提交为独立候选和异人审批","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"approveProfileCandidate","description":"由不同主体批准 Platform Profile 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"activateProfileCandidate","description":"执行 Platform Catalog、Deployment、readiness 与凭证激活 Saga","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"abortProfileCandidate","description":"放弃待审批或已审批的 Platform Profile 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"submitHotServiceDraft","description":"向目标插件 configuration.v1 控制器准备 Hot Service 候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"approveHotServiceCandidate","description":"由不同主体批准 Hot Service 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"activateHotServiceCandidate","description":"激活候选凭证并原子提交目标插件 Hot Service 配置","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"abortHotServiceCandidate","description":"放弃尚未提交的 Hot Service 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"submitScopedDraft","description":"提交 Tenant/User Scoped Hot 候选进入异人审批","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"approveScopedCandidate","description":"由不同主体批准 Scoped Hot 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"activateScopedCandidate","description":"以 Active CAS 原子提交 Scoped Hot 配置","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"abortScopedCandidate","description":"放弃待审批或已审批的 Scoped Hot 配置候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"listResourceItems","description":"列出独立配置资源的非敏感值与凭证状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"resourceCollectionId":{"type":"string"},"catalogDigest":{"type":"string"},"cursor":{"type":"string"},"limit":{"type":"integer","minimum":1,"maximum":256}},"required":["configurationId","resourceCollectionId","catalogDigest"]}},
		{"name":"getResourceItem","description":"读取一个独立配置资源的非敏感值与凭证状态","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"resourceCollectionId":{"type":"string"},"resourceId":{"type":"string"},"catalogDigest":{"type":"string"}},"required":["configurationId","resourceCollectionId","resourceId","catalogDigest"]}},
		{"name":"createResourceDraft","description":"创建独立配置资源草稿并暂存只写秘密","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"resourceCollectionId":{"type":"string"},"catalogDigest":{"type":"string"},"values":{"type":"object"},"secrets":{"type":"object","additionalProperties":{"type":"string"}}},"required":["configurationId","resourceCollectionId","catalogDigest","values"]}},
		{"name":"updateResourceDraft","description":"按 Active CAS 创建独立配置资源更新草稿","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"resourceCollectionId":{"type":"string"},"resourceId":{"type":"string"},"catalogDigest":{"type":"string"},"values":{"type":"object"},"secrets":{"type":"object","additionalProperties":{"type":"string"}}},"required":["configurationId","resourceCollectionId","resourceId","catalogDigest","values"]}},
		{"name":"deleteResourceDraft","description":"按 Active CAS 创建独立配置资源删除草稿","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"configurationId":{"type":"string"},"resourceCollectionId":{"type":"string"},"resourceId":{"type":"string"},"catalogDigest":{"type":"string"}},"required":["configurationId","resourceCollectionId","resourceId","catalogDigest"]}},
		{"name":"submitResourceDraft","description":"向 configuration.resource.v1 提交独立资源候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"approveResourceCandidate","description":"由不同主体批准独立配置资源候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"activateResourceCandidate","description":"提交独立资源并激活候选凭证","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}},
		{"name":"abortResourceCandidate","description":"终止尚未提交的独立配置资源候选","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"id":{"type":"string"},"expectedRevision":{"type":"integer","minimum":1}},"required":["id","expectedRevision"]}}
	]}`)
}
