package nodeagent

import (
	"context"
	"testing"

	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

func TestRegisterRuntimeHostServicesKeepsTrustedServicesOutOfUnitAssembly(t *testing.T) {
	host, err := hostfactory.New("1.0.0", nil)
	if err != nil {
		t.Fatal(err)
	}
	service := func(context.Context, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error) {
		return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, nil, nil
	}
	if err := registerRuntimeHostServices(host, map[string]protocolbus.HostService{"kernel.portal.test": service}); err != nil {
		t.Fatal(err)
	}
	if _, ok := host.Registry.Lookup(extpoint.KernelService, "kernel.portal.test"); !ok {
		t.Fatal("附加宿主服务未注册")
	}
	if err := registerRuntimeHostServices(host, map[string]protocolbus.HostService{"kernel.portal.test": service}); err == nil {
		t.Fatal("重复宿主服务必须拒绝")
	}
}
