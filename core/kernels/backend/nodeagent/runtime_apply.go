package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/pluginconfig"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/servicemodel"
)

type applyTransaction struct {
	runtime       *ProtocolRuntime
	unit          RuntimeUnit
	policy        servicemodel.Policy
	envelope      pluginconfig.Envelope
	candidate     *protocolbus.Host
	instances     []*protocolbus.PluginInstance
	prepared      []preparedMigration
	leaderships   []*controlplane.Leadership
	registrations []*addressing.Registration
	handoffOld    *runningUnit
	committed     bool
}

func (r *ProtocolRuntime) Apply(ctx context.Context, unit RuntimeUnit) (applyErr error) {
	if r.IsRunning(unit.ID, unit.Fingerprint) {
		return nil
	}
	transaction, err := newApplyTransaction(ctx, r, unit)
	if err != nil {
		return err
	}
	defer transaction.rollback(&applyErr)
	if err := transaction.startPlugins(ctx); err != nil {
		return err
	}
	if err := transaction.commitMigrations(ctx); err != nil {
		return err
	}
	if err := transaction.acquireOwnership(ctx); err != nil {
		return err
	}
	if err := transaction.prepareRouting(ctx); err != nil {
		return err
	}
	old, current, err := transaction.commit(ctx)
	if err != nil {
		return err
	}
	transaction.startMonitors(current)
	transaction.retire(ctx, old)
	return nil
}

func newApplyTransaction(ctx context.Context, runtime *ProtocolRuntime, unit RuntimeUnit) (*applyTransaction, error) {
	policy, err := unitPolicy(deploymentUnitForRuntime(unit))
	if err != nil {
		return nil, err
	}
	if err := validateInstalledPolicies(policy, unit.Plugins); err != nil {
		return nil, err
	}
	degraded, err := validateRuntimeRequirements(ctx, unit.Plugins, runtime.router, runtime.DependencyTimeout)
	if err != nil {
		return nil, err
	}
	for _, message := range degraded {
		if runtime.Logf != nil {
			runtime.Logf("unit %s 依赖降级: %s", unit.ID, message)
		}
	}
	pluginRefs := make([]deploymentv1.PluginRef, 0, len(unit.Plugins))
	for _, installed := range unit.Plugins {
		pluginRefs = append(pluginRefs, deploymentv1.PluginRef{ID: installed.ID})
	}
	envelope, err := configEnvelope(unit.Config, pluginRefs)
	if err != nil {
		return nil, fmt.Errorf("解析 unit 配置信封: %w", err)
	}
	configProvider, err := kernelspi.NewPluginMapConfig(envelope.Plugins)
	if err != nil {
		return nil, fmt.Errorf("冻结 unit 配置: %w", err)
	}
	credentialRefs, err := kernelspi.NewPluginMapManagedCredentialRefs(envelope.ManagedCredentials)
	if err != nil {
		return nil, fmt.Errorf("冻结 unit 托管凭证引用: %w", err)
	}
	dependencies := runtime.Dependencies
	dependencies.Config, dependencies.ManagedCredentialRefs = configProvider, credentialRefs
	candidate, err := hostfactory.NewWithDependencies(runtime.KernelVersion, runtime.Logf, dependencies)
	if err != nil {
		return nil, fmt.Errorf("创建候选宿主: %w", err)
	}
	if err := registerRuntimeHostServices(candidate, runtime.HostServices); err != nil {
		return nil, fmt.Errorf("注册候选宿主服务: %w", err)
	}
	if runtime.router != nil {
		candidate.SetCapabilityForwarder(runtime.router.Invoke)
	}
	if err := candidate.Start(); err != nil {
		return nil, err
	}
	return &applyTransaction{runtime: runtime, unit: unit, policy: policy, envelope: envelope, candidate: candidate}, nil
}

func (transaction *applyTransaction) startPlugins(ctx context.Context) error {
	transaction.instances = make([]*protocolbus.PluginInstance, 0, len(transaction.unit.Plugins))
	for _, plugin := range transaction.unit.Plugins {
		instance, err := transaction.startPlugin(ctx, plugin)
		if err != nil {
			return err
		}
		transaction.instances = append(transaction.instances, instance)
	}
	for _, instance := range transaction.instances {
		if !instance.Alive() {
			return fmt.Errorf("候选插件 %s@%s 在发布能力前已退出: %v", instance.PluginID, instance.Version, instance.Err())
		}
	}
	return nil
}

func (transaction *applyTransaction) startPlugin(ctx context.Context, plugin InstalledPlugin) (*protocolbus.PluginInstance, error) {
	values := transaction.envelope.Plugins[plugin.ID]
	if values == nil {
		values = map[string]any{}
	}
	autonomousTenantID, err := backgroundServiceTenant(plugin, values)
	if err != nil {
		return nil, err
	}
	startupConfig, err := json.Marshal(values)
	if err != nil {
		return nil, fmt.Errorf("序列化插件 %s 启动配置: %w", plugin.ID, err)
	}
	runtimeInstanceID, err := newRuntimeInstanceID()
	if err != nil {
		return nil, fmt.Errorf("生成插件 %s 运行实例身份: %w", plugin.ID, err)
	}
	platformGrants, platformConfigured := transaction.unit.KernelServiceGrants[plugin.ID]
	kernelServices, err := transaction.runtime.GrantPolicy.Compile(
		plugin.ID, plugin.Publisher, plugin.Contract.KernelServices, platformGrants, platformConfigured,
	)
	if err != nil {
		return nil, fmt.Errorf("编译插件 %s Capability Grant Plan: %w", plugin.ID, err)
	}
	instance, err := transaction.runtime.startPlugin(ctx, transaction.candidate, plugin, protocolbus.LaunchPolicy{
		PluginID: plugin.ID, Publisher: plugin.Publisher, Version: plugin.Version, ArtifactChannel: plugin.Channel,
		ArtifactSHA256: plugin.SHA256, NodeID: transaction.runtime.Identity, RuntimeInstanceID: runtimeInstanceID,
		Contributions: plugin.Contract.Contributions, KernelServices: kernelServices,
		ContextAccess: plugin.Contract.ContextAccess, ContextCeiling: transaction.runtime.ContextPolicy.Ceiling(plugin.Publisher).Strings(),
		EnvironmentAllowlist: append([]string(nil), transaction.unit.EnvironmentAllowlists[plugin.ID]...),
		Configuration:        startupConfig, RequiredFeatures: append([]string(nil), plugin.Execution.Features...),
		RuntimeScope: transaction.unit.ID, RuntimeGeneration: transaction.unit.Fingerprint,
		BackgroundService: plugin.Contract.BackgroundService, AutonomousTenantID: autonomousTenantID,
	})
	if err != nil {
		return nil, fmt.Errorf("启动插件 %s@%s: %w", plugin.ID, plugin.Version, err)
	}
	if instance.PluginID != plugin.ID || instance.Version != plugin.Version {
		return nil, fmt.Errorf("候选进程身份与验签清单不一致: 期望 %s@%s，实际 %s@%s", plugin.ID, plugin.Version, instance.PluginID, instance.Version)
	}
	return instance, nil
}

func (transaction *applyTransaction) commitMigrations(ctx context.Context) error {
	prepared, err := prepareMigrations(ctx, transaction.candidate, transaction.unit.Migrations, transaction.instances)
	if err != nil {
		return err
	}
	transaction.prepared = prepared
	for _, migration := range prepared {
		if err := transaction.candidate.Migrate(ctx, migration.instance, migrationRequest(migration.plan, protocolbus.MigrationCommit)); err != nil {
			return &StateMigrationError{PluginID: migration.plan.PluginID, Phase: "commit", Err: err}
		}
	}
	return nil
}

func (transaction *applyTransaction) acquireOwnership(ctx context.Context) error {
	if transaction.policy.InstancePolicy != servicemodel.PolicyLeader && transaction.policy.InstancePolicy != servicemodel.PolicyPartitioned {
		return nil
	}
	runtime := transaction.runtime
	runtime.mu.RLock()
	transaction.handoffOld = runtime.units[transaction.unit.ID]
	runtime.mu.RUnlock()
	if transaction.handoffOld != nil {
		runtime.mu.Lock()
		registrations, leaderships := transaction.handoffOld.registrations, transaction.handoffOld.leaderships
		transaction.handoffOld.registrations, transaction.handoffOld.leaderships = nil, nil
		runtime.mu.Unlock()
		closeRegistrations(ctx, registrations)
		for _, leadership := range leaderships {
			_ = leadership.Close(context.Background())
		}
	}
	unit, leaderships, err := runtime.acquireUnitLeaderships(ctx, transaction.unit, transaction.policy)
	if err != nil {
		return err
	}
	transaction.unit, transaction.leaderships = unit, leaderships
	return nil
}

func (transaction *applyTransaction) prepareRouting(ctx context.Context) error {
	if err := bindExecutionFence(transaction.candidate, transaction.unit, transaction.policy, transaction.leaderships); err != nil {
		return err
	}
	transaction.runtime.mu.RLock()
	router := transaction.runtime.router
	transaction.runtime.mu.RUnlock()
	registrations, err := registerCandidate(ctx, router, transaction.candidate, transaction.unit, transaction.instances)
	if err != nil {
		return err
	}
	transaction.registrations = registrations
	return nil
}

func (transaction *applyTransaction) commit(ctx context.Context) (*runningUnit, *runningUnit, error) {
	runtime := transaction.runtime
	runtime.mu.Lock()
	defer runtime.mu.Unlock()
	if runtime.closed {
		return nil, nil, errors.New("运行时已关闭")
	}
	if err := addressing.ActivateRegistrations(ctx, transaction.registrations); err != nil {
		return nil, nil, fmt.Errorf("激活 unit %s 候选能力组: %w", transaction.unit.ID, err)
	}
	old, hadOld := runtime.units[transaction.unit.ID]
	restarts := transaction.unit.RestartBase
	if hadOld && old.fingerprint == transaction.unit.Fingerprint {
		if old.restarts > restarts {
			restarts = old.restarts
		}
		restarts++
	}
	runtime.nextID++
	current := &runningUnit{
		fingerprint: transaction.unit.Fingerprint, host: transaction.candidate, instances: transaction.instances,
		registrations: transaction.registrations, startedAt: time.Now().UTC(), restarts: restarts,
		generation: runtime.nextID, leaderships: transaction.leaderships,
		plugins: append([]InstalledPlugin(nil), transaction.unit.Plugins...), spec: cloneRuntimeUnit(transaction.unit),
	}
	runtime.units[transaction.unit.ID] = current
	transaction.committed = true
	return old, current, nil
}

func (transaction *applyTransaction) startMonitors(current *runningUnit) {
	for _, instance := range transaction.instances {
		go transaction.runtime.monitor(transaction.unit.ID, current.generation, instance)
	}
	for _, leadership := range transaction.leaderships {
		go transaction.runtime.monitorLeadership(transaction.unit.ID, current.generation, leadership)
	}
	go transaction.runtime.monitorDependencies(transaction.unit.ID, current.generation)
}

func (transaction *applyTransaction) retire(ctx context.Context, old *runningUnit) {
	if old == nil {
		return
	}
	closeRegistrations(ctx, old.registrations)
	if err := old.host.Drain(ctx); err != nil && transaction.runtime.Logf != nil {
		transaction.runtime.Logf("旧 unit %s drain 未完整完成，将强制回收: %v", transaction.unit.ID, err)
	}
	old.host.Stop()
	for _, leadership := range old.leaderships {
		_ = leadership.Close(context.Background())
	}
}

func (transaction *applyTransaction) rollback(applyErr *error) {
	if transaction == nil || transaction.committed {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	closeRegistrations(cleanupCtx, transaction.registrations)
	cancel()
	for _, leadership := range transaction.leaderships {
		_ = leadership.Close(context.Background())
	}
	if transaction.handoffOld != nil {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := transaction.runtime.restoreOwnership(restoreCtx, transaction.unit.ID, transaction.handoffOld); err != nil && transaction.runtime.Logf != nil {
			transaction.runtime.Logf("unit %s 候选失败后恢复旧 owner 失败: %v", transaction.unit.ID, err)
		}
		cancel()
	}
	if err := rollbackMigrations(transaction.candidate, transaction.prepared, transaction.runtime.Logf); err != nil {
		*applyErr = errors.Join(*applyErr, err)
	}
	transaction.candidate.Stop()
}
