package nodeagent

import (
	"context"
	"fmt"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
)

func registerCandidate(ctx context.Context, router *addressing.Router, host *protocolbus.Host, unit RuntimeUnit, instances []*protocolbus.PluginInstance) ([]*addressing.Registration, error) {
	if router == nil {
		return nil, nil
	}
	versions := make(map[string]string, len(instances))
	audiences := make(map[string]string, len(instances))
	for _, instance := range instances {
		versions[instance.PluginID] = instance.Version
		audiences[instance.PluginID] = instance.RuntimeAudience
	}
	policies := make(map[string]pluginv1.RuntimeContribution)
	for _, plugin := range unit.Plugins {
		for _, contribution := range plugin.Contract.Contributions {
			policies[plugin.ID+"\x00"+contribution.ExtensionPoint+"\x00"+contribution.ID] = contribution
		}
	}
	logicalService := unit.LogicalService
	if logicalService == "" {
		logicalService = unit.ID
	}
	var registrations []*addressing.Registration
	for _, point := range host.Registry.Points() {
		if point.Dispatch != registry.DispatchSingle {
			continue
		}
		for _, contribution := range host.Registry.List(point.Name) {
			if contribution.PluginID == protocolbus.KernelPluginID {
				continue
			}
			declared := policies[contribution.PluginID+"\x00"+point.Name+"\x00"+contribution.ID]
			instanceID := audiences[contribution.PluginID]
			if instanceID == "" {
				return nil, fmt.Errorf("插件 %s 缺少可信 Runtime audience", contribution.PluginID)
			}
			policy, err := contributionPolicy(declared)
			if err != nil {
				return nil, err
			}
			if policy.Visibility == servicemodel.VisibilityLocal {
				registration, err := router.PrepareLocalRegistration(ctx, addressing.RegisterOptions{
					Capability: contribution.ID, ExtensionPoint: point.Name, ServiceRole: unit.ServiceRole,
					LogicalService: logicalService, RoutingDomain: policy.RoutingDomain,
					InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
					Visibility: policy.Visibility, Routing: policy.Routing, UnitID: unit.ID,
					Version: versions[contribution.PluginID], InstanceID: instanceID,
				}, addressing.HostHandler(func(invokeCtx context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
					response, err := host.Invoke(invokeCtx, target, callCtx, payload)
					if err != nil {
						return nil, nil, err
					}
					return response.Result, response.Payload, nil
				}))
				if err != nil {
					closeRegistrations(context.Background(), registrations)
					return nil, fmt.Errorf("注册 unit %s local capability %s: %w", unit.ID, contribution.ID, err)
				}
				registrations = append(registrations, registration)
				continue
			}
			partitionKeys := []string{""}
			if policy.InstancePolicy == servicemodel.PolicyPartitioned {
				partitionKeys = unit.PartitionKeys
			}
			for _, partitionKey := range partitionKeys {
				routingInstanceID := instanceID
				if partitionKey != "" {
					routingInstanceID += ":partition:" + partitionKey
				}
				generation := unit.Generation
				fencingToken := unit.FencingToken
				if unit.PartitionGenerations != nil {
					generation = unit.PartitionGenerations[partitionKey]
				}
				if unit.PartitionFencingTokens != nil {
					fencingToken = unit.PartitionFencingTokens[partitionKey]
				}
				registration, err := router.PrepareRegistration(ctx, addressing.RegisterOptions{
					Capability: contribution.ID, ExtensionPoint: point.Name,
					ServiceRole: unit.ServiceRole, LogicalService: logicalService, PartitionKey: partitionKey,
					InstancePolicy: policy.InstancePolicy, StateModel: policy.StateModel,
					Visibility: policy.Visibility, Routing: policy.Routing,
					RoutingDomain: policy.RoutingDomain,
					Generation:    generation, FencingToken: fencingToken,
					UnitID: unit.ID, Version: versions[contribution.PluginID], InstanceID: routingInstanceID,
				}, addressing.HostHandler(func(invokeCtx context.Context, target *contractv1.CallTarget, callCtx *contractv1.CallContext, payload []byte) (*contractv1.CallResult, []byte, error) {
					response, err := host.Invoke(invokeCtx, target, callCtx, payload)
					if err != nil {
						return nil, nil, err
					}
					return response.Result, response.Payload, nil
				}))
				if err != nil {
					closeRegistrations(context.Background(), registrations)
					return nil, fmt.Errorf("发布 unit %s capability %s partition=%s: %w", unit.ID, contribution.ID, partitionKey, err)
				}
				registrations = append(registrations, registration)
			}
		}
	}
	return registrations, nil
}

func closeRegistrations(ctx context.Context, registrations []*addressing.Registration) {
	for _, registration := range registrations {
		_ = registration.Close(ctx)
	}
}
