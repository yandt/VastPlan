package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	databasev1 "cdsoft.com.cn/VastPlan/contracts/schemas/database/v1"
	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func callCredential(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "platform.credentials", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: credentialCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return errors.New(result.Error.Message)
		}
		return errors.New("凭证插件拒绝托管凭证操作")
	}
	if output != nil {
		return json.Unmarshal(raw, output)
	}
	return nil
}

func callRuntime(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, input, output any) error {
	payload, err := json.Marshal(input)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "foundation.data.relational.runtime", "platform"
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: databasev1.Capability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}, call, payload)
	if err != nil {
		return err
	}
	if result == nil || result.GetStatus() != contractv1.CallResult_STATUS_OK {
		if result.GetError().GetCode() != "" {
			return fmt.Errorf("Database Runtime %s: %s", result.GetError().GetCode(), result.GetError().GetMessage())
		}
		return errors.New("Database Runtime 暂不可用")
	}
	if output != nil {
		return json.Unmarshal(raw, output)
	}
	return nil
}

func defaultPoolPolicy() databasev1.PoolPolicy {
	return databasev1.PoolPolicy{
		MinIdle: 0, MaxIdle: 8, MaxOpen: 32, MaxLifetimeMS: 30 * 60_000,
		MaxIdleTimeMS: 5 * 60_000, AcquireTimeoutMS: 5_000, IdlePoolTTLMS: 15 * 60_000,
	}
}

func connectionResourceID(tenantID, name string) string {
	digest := sha256.Sum256([]byte(tenantID + "\x00" + name))
	return fmt.Sprintf("connection-%x", digest[:12])
}

func connectionSpec(value definition) databasev1.ConnectionSpec {
	return databasev1.ConnectionSpec{
		Ref:        databasev1.ConnectionRef{ResourceID: value.ResourceID, Revision: value.Revision},
		ProviderID: value.ProviderID, Endpoint: value.Endpoint, Database: value.Database,
		Options: append(json.RawMessage(nil), value.Options...), Credentials: value.CredentialRef, Pool: value.Pool,
	}
}
