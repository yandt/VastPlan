package snapshot

import (
	"context"
	"encoding/json"

	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func (m *Materializer) Contribution() sdk.Contribution {
	descriptor := []byte(`{"title":"Trivy 数据库 File Snapshot","subcommands":[{"name":"status","description":"读取本节点已验证数据库 revision"}]}`)
	return sdk.Contribution{ExtensionPoint: extpoint.ToolPackage, ID: Capability, Descriptor: descriptor, Handlers: map[string]sdk.Handler{
		"status": func(context.Context, sdk.Host, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
			raw, err := json.Marshal(m.Current())
			return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, err
		},
	}}
}

func (m *Materializer) Lifecycle() sdk.LifecycleHandler {
	return func(_ context.Context, lifecycle *pluginhostv1.Lifecycle) error {
		if lifecycle.GetOp() == pluginhostv1.Lifecycle_OP_ACTIVATE {
			_, err := m.Materialize()
			return err
		}
		return nil
	}
}
