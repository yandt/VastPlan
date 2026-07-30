// Command connectionmanager 启动数据库连接定义与受控连通性检查插件进程。
package main

import (
	"context"
	"log"
	"os"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const id, version, capability = "cn.vastplan.platform.data.relational.connection-manager", "0.9.9", "platform.database"

const credentialCapability = "platform.credentials"

func main() {
	service, err := newService(os.Getenv("VASTPLAN_DATABASE_CONNECTIONS_STATE_FILE"))
	if err != nil {
		log.Fatal(err)
	}
	plugin := sdk.New(id, version, map[string]string{"backend": "^0.1"})
	handler := func(operation string) sdk.Handler {
		return func(ctx context.Context, host sdk.Host, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
			return service.handle(ctx, host, call, payload, operation)
		}
	}
	plugin.Contribute(sdk.Contribution{
		ExtensionPoint: extpoint.ToolPackage,
		ID:             capability,
		Descriptor:     descriptor(),
		Handlers: map[string]sdk.Handler{
			"define": handler("define"), "describe": handler("describe"), "list": handler("list"),
			"remove": handler("remove"), "probe": handler("probe"), "resolveRuntime": handler("resolveRuntime"),
		},
	})
	if err := plugin.Serve(); err != nil {
		log.Fatal(err)
	}
}

func descriptor() []byte {
	return []byte(`{"title":"数据库连接","subcommands":[
		{"name":"define","description":"定义连接、Provider 参数和连接池策略，并发布到 Database Runtime","paramsSchema":{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"providerId":{"type":"string"},"endpoint":{"type":"string"},"database":{"type":"string"},"options":{"type":"object"},"pool":{"type":"object"},"credentialValue":{"type":"string"}},"required":["name","providerId","endpoint","options"]}},
		{"name":"describe","description":"读取连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"list","description":"列出连接定义","paramsSchema":{"type":"object","properties":{}}},
		{"name":"remove","description":"删除连接定义","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}},
		{"name":"probe","description":"由 Database Runtime 使用托管凭证探测连接","paramsSchema":{"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}}
	]}`)
}
