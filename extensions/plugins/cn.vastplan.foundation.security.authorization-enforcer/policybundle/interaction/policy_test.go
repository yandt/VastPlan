package interaction

import (
	"testing"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/interactionapi"
)

func TestInteractionPolicySeparatesSourceAndRendererEntry(t *testing.T) {
	request := extpoint.PermissionRequest{Capability: interactionapi.Capability, Operation: "open"}
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_USER, "alice"), request); decision != extpoint.DecisionDeny {
		t.Fatalf("用户不得创建交互: %s", decision)
	}
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_RUNNER, "cn.vastplan.desktop.workflow"), request); decision != extpoint.DecisionAllow {
		t.Fatalf("Desktop 应可创建交互: %s", decision)
	}
	request.Operation = "respond"
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_USER, "alice"), request); decision != extpoint.DecisionAllow {
		t.Fatalf("已认证用户应能进入 Broker 对象校验: %s", decision)
	}
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_PLUGIN, "other"), request); decision != extpoint.DecisionDeny {
		t.Fatalf("插件不得冒充呈现端: %s", decision)
	}
}

func TestInteractionPolicyRestrictsBrokerHostConfig(t *testing.T) {
	request := extpoint.PermissionRequest{Capability: "kernel.config.get", Operation: "get"}
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_PLUGIN, BrokerPluginID()), request); decision != extpoint.DecisionAllow {
		t.Fatalf("Broker 应能读取自身部署配置: %s", decision)
	}
	if decision, _ := decide(contextFor(contractv1.CallerKind_CALLER_KIND_PLUGIN, "other"), request); decision != extpoint.DecisionAbstain {
		t.Fatalf("其他插件不应由交互策略放行: %s", decision)
	}
}

func TestInteractionPolicyAbstainsFromUnrelatedCapabilitiesWithoutInspectingTheirContext(t *testing.T) {
	request := extpoint.PermissionRequest{Capability: "foundation.state.shared.sql.bootstrap", Operation: "test"}
	for name, callCtx := range map[string]*contractv1.CallContext{
		"nil context":      nil,
		"global system":    {Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: "platform-control-bootstrap/primary"}},
		"anonymous global": {},
	} {
		t.Run(name, func(t *testing.T) {
			if decision, _ := decide(callCtx, request); decision != extpoint.DecisionAbstain {
				t.Fatalf("交互策略不得检查或拒绝无关能力: %s", decision)
			}
		})
	}
}

func contextFor(kind contractv1.CallerKind, id string) *contractv1.CallContext {
	return &contractv1.CallContext{TenantId: "tenant-a", Caller: &contractv1.Caller{Kind: kind, Id: id}}
}
