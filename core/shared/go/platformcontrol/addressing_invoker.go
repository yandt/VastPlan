package platformcontrol

import (
	"context"
	"errors"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	platformcontrolv1 "cdsoft.com.cn/VastPlan/contracts/schemas/platformcontrol/v1"
	sharedstatesqlv1 "cdsoft.com.cn/VastPlan/contracts/schemas/sharedstatesql/v1"
	"cdsoft.com.cn/VastPlan/core/internal/callcontext"
	platformcontrolport "cdsoft.com.cn/VastPlan/extensions/libraries/go/platformcontrol"
)

type AddressingRouter interface {
	Invoke(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
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
	if capability != platformcontrolv1.BootstrapCapability && capability != sharedstatesqlv1.Capability {
		return nil, nil, errors.New("Platform Control Invoker 拒绝未知 capability")
	}
	wire := &contractv1.CallContext{Caller: &contractv1.Caller{
		Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: platformcontrolv1.TrustedBootstrapSystemID,
	}, Scene: "platform.control.bootstrap"}
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
	return i.router.Invoke(callcontext.WithTrusted(ctx, trusted), target, trusted.Wire(), payload)
}

func stringPointer(value string) *string { return &value }

var _ platformcontrolport.Invoker = (*AddressingInvoker)(nil)
