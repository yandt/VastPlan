package nodeagent

import (
	"context"
	"errors"
	"fmt"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/operationfence"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

func deploymentUnitForRuntime(unit RuntimeUnit) deploymentv1.Unit {
	return deploymentv1.Unit{
		ID: unit.ID, InstancePolicy: unit.InstancePolicy, StateModel: unit.StateModel,
		Visibility: unit.Visibility, Routing: unit.Routing, RoutingDomain: unit.RoutingDomain,
	}
}

func (r *ProtocolRuntime) acquireUnitLeaderships(ctx context.Context, unit RuntimeUnit, policy servicemodel.Policy) (RuntimeUnit, []*controlplane.Leadership, error) {
	if r.LeaderKV == nil {
		return unit, nil, errors.New("leader unit 未配置控制面 lease KV")
	}
	logicalService := unit.LogicalService
	if logicalService == "" {
		logicalService = unit.ID
	}
	identity := r.Identity
	if identity == "" {
		identity = unit.ID
	}
	keys := []string{""}
	if policy.InstancePolicy == servicemodel.PolicyPartitioned {
		keys = append([]string(nil), unit.PartitionKeys...)
		if len(keys) == 0 {
			return unit, nil, errors.New("partitioned unit 必须配置至少一个 partition key")
		}
	}
	unit.PartitionGenerations = map[string]uint64{}
	unit.PartitionFencingTokens = map[string]string{}
	unit.Generation, unit.FencingToken = 0, ""
	leaderships := make([]*controlplane.Leadership, 0, len(keys))
	for _, partitionKey := range keys {
		election := "plugin/" + logicalService + "/" + unit.RoutingDomain
		if partitionKey != "" {
			election += "/partition/" + partitionKey
		}
		elector := controlplane.LeaderElector{KV: r.LeaderKV, Election: election, Identity: identity + "/" + unit.ID + "/" + partitionKey, Options: controlplane.LeaderElectionOptions{Logf: r.Logf}}
		leadership, err := elector.Acquire(ctx)
		if err != nil {
			for _, previous := range leaderships {
				_ = previous.Close(context.Background())
			}
			return unit, nil, fmt.Errorf("unit %s 获取 %s lease: %w", unit.ID, election, err)
		}
		leaderships = append(leaderships, leadership)
		record := leadership.Record()
		unit.PartitionGenerations[partitionKey] = record.Epoch
		unit.PartitionFencingTokens[partitionKey] = record.Token
		if unit.Generation == 0 || record.Epoch < unit.Generation {
			unit.Generation = record.Epoch
		}
		if unit.FencingToken == "" {
			unit.FencingToken = record.Token
		}
	}
	return unit, leaderships, nil
}

func (r *ProtocolRuntime) restoreOwnership(ctx context.Context, unitID string, old *runningUnit) error {
	if old == nil {
		return nil
	}
	policy, err := unitPolicy(deploymentUnitForRuntime(old.spec))
	if err != nil {
		return err
	}
	restoredSpec, leaderships, err := r.acquireUnitLeaderships(ctx, cloneRuntimeUnit(old.spec), policy)
	if err != nil {
		return err
	}
	if err := bindExecutionFence(old.host, restoredSpec, policy, leaderships); err != nil {
		for _, leadership := range leaderships {
			_ = leadership.Close(context.Background())
		}
		return err
	}
	r.mu.RLock()
	router := r.router
	r.mu.RUnlock()
	registrations, err := registerCandidate(ctx, router, old.host, restoredSpec, old.instances)
	if err == nil {
		err = addressing.ActivateRegistrations(ctx, registrations)
	}
	if err != nil {
		closeRegistrations(context.Background(), registrations)
		for _, leadership := range leaderships {
			_ = leadership.Close(context.Background())
		}
		return err
	}
	r.mu.Lock()
	current, exists := r.units[unitID]
	if !exists || current != old {
		r.mu.Unlock()
		closeRegistrations(context.Background(), registrations)
		for _, leadership := range leaderships {
			_ = leadership.Close(context.Background())
		}
		return errors.New("旧 owner 已不再是当前 unit")
	}
	old.registrations, old.leaderships, old.spec = registrations, leaderships, restoredSpec
	r.mu.Unlock()
	for _, leadership := range leaderships {
		go r.monitorLeadership(unitID, old.generation, leadership)
	}
	return nil
}

type leadershipFenceProvider struct {
	leadership     *controlplane.Leadership
	logicalService string
	unitID         string
}

func (p leadershipFenceProvider) Current() (operationfence.Evidence, bool) {
	if p.leadership == nil {
		return operationfence.Evidence{}, false
	}
	record, ok := p.leadership.Current()
	if !ok {
		return operationfence.Evidence{}, false
	}
	evidence := operationfence.Evidence{LogicalService: p.logicalService, UnitID: p.unitID, Epoch: record.Epoch, Token: record.Token}
	return evidence, evidence.Validate() == nil
}

func bindExecutionFence(host *protocolbus.Host, unit RuntimeUnit, policy servicemodel.Policy, leaderships []*controlplane.Leadership) error {
	if policy.InstancePolicy != servicemodel.PolicyLeader {
		return nil
	}
	if host == nil || len(leaderships) != 1 {
		return errors.New("leader runtime 缺少唯一 execution fence")
	}
	logicalService := unit.LogicalService
	if logicalService == "" {
		logicalService = unit.ID
	}
	host.SetExecutionFenceProvider(leadershipFenceProvider{leadership: leaderships[0], logicalService: logicalService, unitID: unit.ID})
	return nil
}
