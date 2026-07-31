package main

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	apiv1 "cdsoft.com.cn/VastPlan/contracts/schemas/api/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

const (
	apiExposureCapability  = "platform.api-exposure"
	uploadEndpointLeaseTTL = 240
)

type uploadLeaseRegistrar struct {
	mu            sync.Mutex
	configuration *staging.DataPlaneConfiguration
	leases        map[string]apiv1.EndpointLease
	lastErrors    map[string]string
}

func (r *uploadLeaseRegistrar) ensure(ctx context.Context, host sdk.Host, call *contractv1.CallContext) {
	if r == nil || r.configuration == nil || host == nil || call == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	tenantID := call.GetTenantId()
	exposureID, configured := r.configuration.ExposureForTenant(tenantID)
	if !configured {
		return
	}
	if r.leases == nil {
		r.leases, r.lastErrors = map[string]apiv1.EndpointLease{}, map[string]string{}
	}
	r.ensureProfile(ctx, host, call, tenantID+"\x00"+r.configuration.InstanceID, exposureID, r.configuration.InstanceID, r.configuration.Endpoint, r.configuration.TLSIdentity, apiv1.ModeTicketRedirect)
	if private := r.configuration.Private; private != nil {
		r.ensureProfile(ctx, host, call, tenantID+"\x00"+private.InstanceID, exposureID, private.InstanceID, private.Endpoint, private.TLSIdentity, apiv1.ModePrivateDirect)
	}
}

func (r *uploadLeaseRegistrar) ensureProfile(ctx context.Context, host sdk.Host, call *contractv1.CallContext, key, exposureID, instanceID, endpoint, identity, mode string) {
	lease := r.leases[key]
	if lease.LeaseID != "" && time.Until(lease.ExpiresAt) > 90*time.Second {
		return
	}
	if lease.LeaseID != "" {
		raw, _ := json.Marshal(apiv1.EndpointLeaseRenewal{LeaseID: lease.LeaseID, TTLSeconds: uploadEndpointLeaseTTL})
		if renewed, err := callUploadEndpointLease(ctx, host, call, "renewEndpointLease", raw); err == nil {
			r.leases[key], r.lastErrors[key] = renewed, ""
			return
		}
		delete(r.leases, key)
	}
	raw, _ := json.Marshal(apiv1.EndpointLeaseRegistration{
		DataPlaneExposureID: exposureID, InstanceID: instanceID, Endpoint: endpoint, TLSIdentity: identity,
		Modes: []string{mode}, TTLSeconds: uploadEndpointLeaseTTL,
	})
	lease, err := callUploadEndpointLease(ctx, host, call, "registerEndpointLease", raw)
	if err != nil {
		r.lastErrors[key] = err.Error()
		return
	}
	r.leases[key], r.lastErrors[key] = lease, ""
}

func callUploadEndpointLease(ctx context.Context, host sdk.Host, call *contractv1.CallContext, operation string, payload []byte) (apiv1.EndpointLease, error) {
	result, raw, err := host.Call(ctx, &contractv1.CallTarget{ExtensionPoint: extpoint.ToolPackage, Capability: apiExposureCapability, Operation: &operation}, call, payload)
	if err != nil || result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		return apiv1.EndpointLease{}, errors.New("Content Upload Endpoint Lease 调用失败")
	}
	var lease apiv1.EndpointLease
	if json.Unmarshal(raw, &lease) != nil || apiv1.ValidateEndpointLease(lease, time.Now().UTC()) != nil {
		return apiv1.EndpointLease{}, errors.New("API Exposure 返回无效 Content Upload Endpoint Lease")
	}
	return lease, nil
}
