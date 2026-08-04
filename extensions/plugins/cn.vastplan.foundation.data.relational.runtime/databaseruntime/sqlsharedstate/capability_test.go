package sqlsharedstate

import (
	"context"
	"encoding/json"
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/recordstore"
)

func TestCapabilityAcceptsOnlyTrustedKernelCalls(t *testing.T) {
	dialect, _ := recordstore.DialectFor("postgresql")
	store, _ := NewStore(dialect, &memorySessions{rows: map[string]stateRow{}})
	service, _ := NewCapabilityService(store)
	request := sharedstatesqlv1.WriteRequest{Scope: sharedstatesqlv1.Scope{Kind: "tenant", TenantID: "tenant-a", PluginID: "cn.vastplan.example", RuntimeScope: "service-a", Namespace: "settings"}, Key: "active", ValueBase64: "e30="}
	payload, _ := json.Marshal(request)
	plugin := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: "cn.vastplan.example"}}
	denied, _, err := service.Contribution().Handlers[sharedstatesqlv1.OperationCreate](context.Background(), nil, plugin, payload)
	if err != nil || denied.GetError().GetCode() != sharedstatesqlv1.ErrorInvalid {
		t.Fatalf("普通插件必须拒绝: %+v %v", denied, err)
	}
	system := &contractv1.CallContext{Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "backend-kernel"}}
	accepted, raw, err := service.Contribution().Handlers[sharedstatesqlv1.OperationCreate](context.Background(), nil, system, payload)
	if err != nil || accepted.GetStatus() != contractv1.CallResult_STATUS_OK {
		t.Fatalf("可信 Kernel 调用失败: %+v %s %v", accepted, raw, err)
	}
	var entry sharedstatesqlv1.Entry
	if json.Unmarshal(raw, &entry) != nil || entry.Revision != 1 {
		t.Fatalf("响应无效: %s", raw)
	}
}

var _ sharedstate.Store = (*Store)(nil)
