package platformcontrol

import (
	"context"
	"errors"
	"sort"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

type AddressingRouter interface {
	Invoke(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
	InstancesFor(string, string, string) []addressing.Announcement
	LocalNodeID() string
}

// AddressingInvoker fixes the trusted caller and routing identity once. The
// bootstrap workflow and RemoteStore cannot supply arbitrary system callers or
// route SQL state to a business Database Runtime service.
type AddressingInvoker struct{ router AddressingRouter }

func NewAddressingInvoker(router AddressingRouter) (*AddressingInvoker, error) {
	if router == nil {
		return nil, errors.New("Platform Control Addressing Invoker 缺少 Router")
	}
	return &AddressingInvoker{router: router}, nil
}

func (i *AddressingInvoker) Invoke(ctx context.Context, capability, operation string, payload []byte) (*contractv1.CallResult, []byte, error) {
	return i.invoke(ctx, capability, operation, "", payload)
}

func (i *AddressingInvoker) InvokeInstance(ctx context.Context, capability, operation, instanceID string, payload []byte) (*contractv1.CallResult, []byte, error) {
	if instanceID == "" {
		return nil, nil, errors.New("Platform Control Invoker 拒绝空 instance_id")
	}
	return i.invoke(ctx, capability, operation, instanceID, payload)
}

func (i *AddressingInvoker) Instances(capability string) []platformcontrolport.RuntimeInstance {
	if !allowedCapability(capability) {
		return nil
	}
	announcements := i.router.InstancesFor(capability, platformcontrolv1.RuntimeLogicalService, platformcontrolv1.RuntimeRoutingDomain)
	instances := make([]platformcontrolport.RuntimeInstance, 0, len(announcements))
	for _, announcement := range announcements {
		instances = append(instances, platformcontrolport.RuntimeInstance{ID: announcement.InstanceID, NodeID: announcement.NodeID})
	}
	localNode := i.router.LocalNodeID()
	sort.Slice(instances, func(left, right int) bool {
		leftLocal, rightLocal := instances[left].NodeID == localNode, instances[right].NodeID == localNode
		if leftLocal != rightLocal {
			return leftLocal
		}
		return instances[left].ID < instances[right].ID
	})
	return instances
}

func (i *AddressingInvoker) invoke(ctx context.Context, capability, operation, instanceID string, payload []byte) (*contractv1.CallResult, []byte, error) {
	if !allowedCapability(capability) {
		return nil, nil, errors.New("Platform Control Invoker 拒绝未知 capability")
	}
	wire := &contractv1.CallContext{Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: platformcontrolv1.TrustedBootstrapSystemID,
	}, Scene: "platform.control.bootstrap"}
	// Keep only the parent's trace correlation. The Bootstrap hop replaces the
	// caller and scene and must not inherit user identity, tenant, credentials or
	// other request authority from the Connection Manager call.
	if parent, ok := callcontext.FromContext(ctx); ok {
		wire.Trace = parent.Wire().GetTrace()
	}
	trusted, err := callcontext.ValidateIngress(wire, callcontext.Provenance{
		Source: "backend.kernel", AuthenticatedBy: "platform-control-controller",
	})
	if err != nil {
		return nil, nil, err
	}
	target := &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage,
		Capability:     capability,
		Operation:      &operation,
		LogicalService: stringPointer(platformcontrolv1.RuntimeLogicalService),
		RoutingDomain:  stringPointer(platformcontrolv1.RuntimeRoutingDomain),
	}
	if instanceID != "" {
		target.InstanceId = stringPointer(instanceID)
	}
	return i.router.Invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
}

func allowedCapability(capability string) bool {
	return capability == platformcontrolv1.BootstrapCapability || capability == sharedstatesqlv1.Capability
}

func stringPointer(value string) *string { return &value }

var _ platformcontrolport.Invoker = (*AddressingInvoker)(nil)
