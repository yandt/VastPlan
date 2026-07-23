//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	configurationscopedv1 "cdsoft.com.cn/VastPlan/contracts/schemas/configurationscoped/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
)

const scopedConfigurationExtensionPoint = configurationscopedv1.ExtensionPoint

func registerHostScopedConfiguration(t *testing.T, host *protocolbus.Host) {
	t.Helper()
	if err := host.RegisterHostService(configurationscopedv1.ExtensionPoint, configurationscopedv1.Capability, scopedConfigurationHandler); err != nil {
		t.Fatalf("注册 E2E Scoped Configuration: %v", err)
	}
}

func registerAddressingScopedConfiguration(t *testing.T, router *addressing.Router) {
	t.Helper()
	registration, err := router.Register(context.Background(), addressing.RegisterOptions{
		Capability: configurationscopedv1.Capability, ExtensionPoint: configurationscopedv1.ExtensionPoint,
		ServiceRole: "backend", LogicalService: "platform.plugin-configuration", RoutingDomain: "platform",
		InstancePolicy: "active-active", StateModel: "external-shared", Visibility: "cluster", Routing: "queue",
		Readiness: "ready", UnitID: "scoped-configuration-e2e", Version: "1.0.0",
	}, func(ctx context.Context, target *contractv1.CallTarget, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
		if target.GetExtensionPoint() != configurationscopedv1.ExtensionPoint || target.GetCapability() != configurationscopedv1.Capability || target.GetOperation() != configurationscopedv1.OperationResolve {
			return nil, nil, fmt.Errorf("E2E Scoped Configuration target 无效")
		}
		return scopedConfigurationHandler(ctx, call, payload)
	})
	if err != nil {
		t.Fatalf("发布 E2E Scoped Configuration: %v", err)
	}
	t.Cleanup(func() { _ = registration.Close(context.Background()) })
}

func scopedConfigurationHandler(_ context.Context, call *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
	if call.GetCaller().GetKind() != contractv1.CallerKind_CALLER_KIND_PLUGIN || call.GetTenantId() == "" {
		return nil, nil, fmt.Errorf("E2E Scoped Configuration 要求认证插件与 tenant")
	}
	if _, err := configurationscopedv1.ParseRequest(configurationscopedv1.OperationResolve, payload); err != nil {
		return nil, nil, err
	}
	values := json.RawMessage(`{"greetingTemplate":"Welcome, {{name}} from E2E"}`)
	digest, err := configurationscopedv1.DigestValues(values)
	if err != nil {
		return nil, nil, err
	}
	raw, err := json.Marshal(configurationscopedv1.Resolution{
		Protocol: configurationscopedv1.Protocol, ConfigurationID: "cfg_" + strings.Repeat("a", 24), Scope: configurationscopedv1.ScopeTenant,
		Revision: 1, Digest: digest, SchemaDigest: strings.Repeat("b", 64), ArtifactSHA256: strings.Repeat("c", 64),
		Values: values, Source: "active", ObservedAt: time.Now().UTC(),
	})
	if err != nil {
		return nil, nil, err
	}
	return &contractv1.CallResult{Status: contractv1.CallResult_STATUS_OK}, raw, nil
}
