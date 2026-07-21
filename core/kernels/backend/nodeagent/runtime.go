package nodeagent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	deploymentv1 "cdsoft.com.cn/VastPlan/contracts/schemas/deployment/v1"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/kernels/backend/hostfactory"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	"cdsoft.com.cn/VastPlan/core/shared/go/kernelspi"
	"cdsoft.com.cn/VastPlan/core/shared/go/protocolbus"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
	"cdsoft.com.cn/VastPlan/core/shared/go/servicemodel"
	"github.com/nats-io/nats.go/jetstream"
)

type runningUnit struct {
	fingerprint      string
	host             *protocolbus.Host
	instances        []*protocolbus.PluginInstance
	registrations    []*addressing.Registration
	startedAt        time.Time
	restarts         uint64
	generation       uint64
	notified         bool
	leaderships      []*controlplane.Leadership
	plugins          []InstalledPlugin
	dependencyIssues []string
	spec             RuntimeUnit
}

// ProtocolRuntime 为每个 service unit 创建独立 backend 宿主。候选宿主先完成全部插件
// 握手和激活，再原子替换 map 中的当前实例，随后才关闭旧宿主。
type ProtocolRuntime struct {
	KernelVersion     string
	Logf              func(string, ...any)
	DependencyTimeout time.Duration
	Identity          string
	LeaderKV          jetstream.KeyValue

	mu              sync.RWMutex
	units           map[string]*runningUnit
	closed          bool
	events          chan RuntimeEvent
	nextID          uint64
	router          *addressing.Router
	Dependencies    kernelspi.Dependencies
	HostServices    map[string]protocolbus.HostService
	Drivers         *ExecutionDriverRegistry
	RuntimePools    *RuntimePoolManager
	ExecutionPolicy ExecutionPolicy
	HostingPolicy   RuntimeHostingPolicy
	ContextPolicy   ContextPolicy
	dynamicGoDriver PluginExecutionDriver
	PlacementPolicy PlacementPolicy
}

// AttachRouter 在首个 unit 启动前接入全局能力寻址。运行中切换 Router 会让已经发布的
// 租约和实际 handler 分离，因此明确拒绝这种隐式重配。
func (r *ProtocolRuntime) AttachRouter(router *addressing.Router) error {
	if router == nil {
		return errors.New("addressing router 不能为空")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("运行时已关闭")
	}
	if len(r.units) != 0 {
		return errors.New("已有 unit 运行时不能接入 addressing router")
	}
	r.router = router
	return nil
}

func NewProtocolRuntime(kernelVersion string, logf func(string, ...any)) *ProtocolRuntime {
	return &ProtocolRuntime{
		KernelVersion:     kernelVersion,
		Logf:              logf,
		DependencyTimeout: 5 * time.Second,
		Drivers:           DefaultExecutionDrivers(),
		RuntimePools:      NewRuntimePoolManager(logf),
		HostingPolicy:     RuntimeHostingPolicy{Default: RuntimeHostingShared},
		ContextPolicy:     DefaultContextPolicy(),
		units:             map[string]*runningUnit{},
		events:            make(chan RuntimeEvent, 64),
	}
}

func (r *ProtocolRuntime) Apply(ctx context.Context, unit RuntimeUnit) (applyErr error) {
	if r.IsRunning(unit.ID, unit.Fingerprint) {
		return nil
	}
	policy, err := unitPolicy(deploymentUnitForRuntime(unit))
	if err != nil {
		return err
	}
	if err := validateInstalledPolicies(policy, unit.Plugins); err != nil {
		return err
	}
	degradedDependencies, err := validateRuntimeRequirements(ctx, unit.Plugins, r.router, r.DependencyTimeout)
	if err != nil {
		return err
	}
	for _, message := range degradedDependencies {
		if r.Logf != nil {
			r.Logf("unit %s 依赖降级: %s", unit.ID, message)
		}
	}
	ok := false
	var leaderships []*controlplane.Leadership
	pluginRefs := make([]deploymentv1.PluginRef, 0, len(unit.Plugins))
	for _, installed := range unit.Plugins {
		pluginRefs = append(pluginRefs, deploymentv1.PluginRef{ID: installed.ID})
	}
	envelope, err := configEnvelope(unit.Config, pluginRefs)
	if err != nil {
		return fmt.Errorf("解析 unit 配置信封: %w", err)
	}
	configProvider, err := kernelspi.NewPluginMapConfig(envelope.Plugins)
	if err != nil {
		return fmt.Errorf("冻结 unit 配置: %w", err)
	}
	dependencies := r.Dependencies
	dependencies.Config = configProvider
	candidate, err := hostfactory.NewWithDependencies(r.KernelVersion, r.Logf, dependencies)
	if err != nil {
		return fmt.Errorf("创建候选宿主: %w", err)
	}
	if err := registerRuntimeHostServices(candidate, r.HostServices); err != nil {
		return fmt.Errorf("注册候选宿主服务: %w", err)
	}
	if r.router != nil {
		candidate.SetCapabilityForwarder(r.router.Invoke)
	}
	if err := candidate.Start(); err != nil {
		return err
	}
	defer func() {
		if !ok {
			candidate.Stop()
		}
	}()
	instances := make([]*protocolbus.PluginInstance, 0, len(unit.Plugins))
	for _, plugin := range unit.Plugins {
		pluginValues := envelope.Plugins[plugin.ID]
		if pluginValues == nil {
			pluginValues = map[string]any{}
		}
		startupConfig, err := json.Marshal(pluginValues)
		if err != nil {
			return fmt.Errorf("序列化插件 %s 启动配置: %w", plugin.ID, err)
		}
		runtimeInstanceID, err := newRuntimeInstanceID()
		if err != nil {
			return fmt.Errorf("生成插件 %s 运行实例身份: %w", plugin.ID, err)
		}
		instance, err := r.startPlugin(ctx, candidate, plugin, protocolbus.LaunchPolicy{
			PluginID: plugin.ID, Publisher: plugin.Publisher, Version: plugin.Version,
			ArtifactSHA256: plugin.SHA256, NodeID: r.Identity, RuntimeInstanceID: runtimeInstanceID,
			Contributions:        plugin.Contract.Contributions,
			KernelServices:       plugin.Contract.KernelServices,
			ContextAccess:        plugin.Contract.ContextAccess,
			ContextCeiling:       r.ContextPolicy.Ceiling(plugin.Publisher).Strings(),
			EnvironmentAllowlist: append([]string(nil), unit.EnvironmentAllowlists[plugin.ID]...),
			Configuration:        startupConfig,
			RequiredFeatures:     append([]string(nil), plugin.Execution.Features...),
			RuntimeScope:         unit.ID,
			RuntimeGeneration:    unit.Fingerprint,
		})
		if err != nil {
			return fmt.Errorf("启动插件 %s@%s: %w", plugin.ID, plugin.Version, err)
		}
		if instance.PluginID != plugin.ID || instance.Version != plugin.Version {
			return fmt.Errorf("候选进程身份与验签清单不一致: 期望 %s@%s，实际 %s@%s",
				plugin.ID, plugin.Version, instance.PluginID, instance.Version)
		}
		instances = append(instances, instance)
	}
	for _, instance := range instances {
		if !instance.Alive() {
			return fmt.Errorf("候选插件 %s@%s 在发布能力前已退出: %v", instance.PluginID, instance.Version, instance.Err())
		}
	}
	prepared, err := prepareMigrations(ctx, candidate, unit.Migrations, instances)
	if err != nil {
		return err
	}
	defer func() {
		if !ok {
			if rollbackErr := rollbackMigrations(candidate, prepared, r.Logf); rollbackErr != nil {
				applyErr = errors.Join(applyErr, rollbackErr)
			}
		}
	}()
	for _, migration := range prepared {
		if err := candidate.Migrate(ctx, migration.instance, migrationRequest(migration.plan, protocolbus.MigrationCommit)); err != nil {
			return &StateMigrationError{PluginID: migration.plan.PluginID, Phase: "commit", Err: err}
		}
	}
	var handoffOld *runningUnit
	if policy.InstancePolicy == servicemodel.PolicyLeader || policy.InstancePolicy == servicemodel.PolicyPartitioned {
		r.mu.RLock()
		handoffOld = r.units[unit.ID]
		r.mu.RUnlock()
		if handoffOld != nil {
			// 候选已经完成启动、健康和迁移后才进入短暂的 fencing 交接窗。
			// 交接后的任何失败都会用旧宿主重新取得租约并恢复原 registration。
			r.mu.Lock()
			oldRegistrations := handoffOld.registrations
			oldLeaderships := handoffOld.leaderships
			handoffOld.registrations, handoffOld.leaderships = nil, nil
			r.mu.Unlock()
			// 先从 runningUnit 摘除，确保主动 Close 触发的 monitor 不会把这次
			// 正常升级交接误判为领导权丢失并停止旧宿主。
			closeRegistrations(ctx, oldRegistrations)
			for _, leadership := range oldLeaderships {
				_ = leadership.Close(context.Background())
			}
			defer func() {
				if !ok {
					restoreCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					if restoreErr := r.restoreOwnership(restoreCtx, unit.ID, handoffOld); restoreErr != nil && r.Logf != nil {
						r.Logf("unit %s 候选失败后恢复旧 owner 失败: %v", unit.ID, restoreErr)
					}
				}
			}()
		}
		unit, leaderships, err = r.acquireUnitLeaderships(ctx, unit, policy)
		if err != nil {
			return err
		}
		defer func() {
			if !ok {
				for _, leadership := range leaderships {
					_ = leadership.Close(context.Background())
				}
			}
		}()
	}
	r.mu.RLock()
	router := r.router
	r.mu.RUnlock()
	registrations, err := registerCandidate(ctx, router, candidate, unit, instances)
	if err != nil {
		return err
	}
	defer func() {
		if !ok {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			closeRegistrations(cleanupCtx, registrations)
		}
	}()

	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("运行时已关闭")
	}
	// 迁移已提交，但 registration 门闩仍关闭。全部租约成功激活后，在同一个
	// Runtime 临界区立即切换当前指针；从此路径起没有可失败步骤。
	if err := addressing.ActivateRegistrations(ctx, registrations); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("激活 unit %s 候选能力组: %w", unit.ID, err)
	}
	old, hadOld := r.units[unit.ID]
	restarts := unit.RestartBase
	if hadOld && old.fingerprint == unit.Fingerprint {
		if old.restarts > restarts {
			restarts = old.restarts
		}
		restarts++
	}
	r.nextID++
	current := &runningUnit{
		fingerprint:   unit.Fingerprint,
		host:          candidate,
		instances:     instances,
		registrations: registrations,
		startedAt:     time.Now().UTC(),
		restarts:      restarts,
		generation:    r.nextID,
		leaderships:   leaderships,
		plugins:       append([]InstalledPlugin(nil), unit.Plugins...),
		spec:          cloneRuntimeUnit(unit),
	}
	r.units[unit.ID] = current
	r.mu.Unlock()
	ok = true
	for _, instance := range instances {
		go r.monitor(unit.ID, current.generation, instance)
	}
	for _, leadership := range leaderships {
		go r.monitorLeadership(unit.ID, current.generation, leadership)
	}
	go r.monitorDependencies(unit.ID, current.generation)
	if hadOld {
		closeRegistrations(ctx, old.registrations)
		if err := old.host.Drain(ctx); err != nil && r.Logf != nil {
			r.Logf("旧 unit %s drain 未完整完成，将强制回收: %v", unit.ID, err)
		}
		old.host.Stop()
		for _, leadership := range old.leaderships {
			_ = leadership.Close(context.Background())
		}
	}
	return nil
}

func registerRuntimeHostServices(host *protocolbus.Host, services map[string]protocolbus.HostService) error {
	names := make([]string, 0, len(services))
	for name := range services {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if name == "" || services[name] == nil {
			return errors.New("附加内核服务名称和实现不能为空")
		}
		if err := host.RegisterHostService(extpoint.KernelService, name, services[name]); err != nil {
			return err
		}
	}
	return nil
}

func newRuntimeInstanceID() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return "runtime-" + hex.EncodeToString(raw), nil
}

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

func cloneRuntimeUnit(unit RuntimeUnit) RuntimeUnit {
	unit.PartitionKeys = append([]string(nil), unit.PartitionKeys...)
	unit.EnvironmentAllowlists = cloneStringSlices(unit.EnvironmentAllowlists)
	unit.Plugins = append([]InstalledPlugin(nil), unit.Plugins...)
	for index := range unit.Plugins {
		unit.Plugins[index].Engines = cloneStringMap(unit.Plugins[index].Engines)
	}
	unit.Migrations = append([]StateMigrationPlan(nil), unit.Migrations...)
	unit.PartitionGenerations = cloneUint64Map(unit.PartitionGenerations)
	unit.PartitionFencingTokens = cloneStringMap(unit.PartitionFencingTokens)
	unit.Config = RawConfig(unit.Config)
	return unit
}

func cloneUint64Map(input map[string]uint64) map[string]uint64 {
	if input == nil {
		return nil
	}
	out := make(map[string]uint64, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func cloneStringSlices(input map[string][]string) map[string][]string {
	if input == nil {
		return nil
	}
	out := make(map[string][]string, len(input))
	for key, values := range input {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

type preparedMigration struct {
	plan     StateMigrationPlan
	instance *protocolbus.PluginInstance
}

func prepareMigrations(ctx context.Context, host *protocolbus.Host, plans []StateMigrationPlan, instances []*protocolbus.PluginInstance) ([]preparedMigration, error) {
	byPlugin := make(map[string]*protocolbus.PluginInstance, len(instances))
	for _, instance := range instances {
		byPlugin[instance.PluginID] = instance
	}
	prepared := make([]preparedMigration, 0, len(plans))
	for _, plan := range plans {
		instance := byPlugin[plan.PluginID]
		if instance == nil {
			err := &StateMigrationError{PluginID: plan.PluginID, Phase: "prepare", Err: errors.New("迁移计划引用未启动的候选插件")}
			return nil, errors.Join(err, rollbackMigrations(host, prepared, nil))
		}
		migration := preparedMigration{plan: plan, instance: instance}
		// 即使 PREPARE 返回错误，也可能已经产生了部分候选状态；先登记再调用，
		// 失败路径才能把本插件一并纳入逆序 ROLLBACK。
		prepared = append(prepared, migration)
		if err := host.Migrate(ctx, instance, migrationRequest(plan, protocolbus.MigrationPrepare)); err != nil {
			prepareErr := &StateMigrationError{PluginID: plan.PluginID, Phase: "prepare", Err: err}
			return nil, errors.Join(prepareErr, rollbackMigrations(host, prepared, nil))
		}
	}
	return prepared, nil
}

func migrationRequest(plan StateMigrationPlan, operation protocolbus.MigrationOperation) protocolbus.MigrationCommand {
	return protocolbus.MigrationCommand{
		Operation: operation, TransactionID: plan.TransactionID,
		From: plan.From.contractIdentity(),
		To:   plan.To.contractIdentity(),
	}
}

func rollbackMigrations(host *protocolbus.Host, prepared []preparedMigration, logf func(string, ...any)) error {
	var rollbackErr error
	for index := len(prepared) - 1; index >= 0; index-- {
		migration := prepared[index]
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := host.Migrate(ctx, migration.instance, migrationRequest(migration.plan, protocolbus.MigrationRollback))
		cancel()
		if err != nil && logf != nil {
			logf("回滚插件 %s 状态迁移失败 transaction=%s: %v",
				migration.plan.PluginID, migration.plan.TransactionID, err)
		}
		if err != nil {
			rollbackErr = errors.Join(rollbackErr, &StateMigrationError{
				PluginID: migration.plan.PluginID, Phase: "rollback", Err: err,
			})
		}
	}
	return rollbackErr
}

func (r *ProtocolRuntime) Stop(ctx context.Context, unitID string) error {
	r.mu.Lock()
	unit, ok := r.units[unitID]
	if ok {
		delete(r.units, unitID)
	}
	r.mu.Unlock()
	if ok {
		closeRegistrations(ctx, unit.registrations)
		if err := unit.host.Drain(ctx); err != nil && r.Logf != nil {
			r.Logf("unit %s drain 未完整完成，将强制回收: %v", unitID, err)
		}
		unit.host.Stop()
		for _, leadership := range unit.leaderships {
			if err := leadership.Close(ctx); err != nil && r.Logf != nil {
				r.Logf("unit %s 释放 leader lease 失败: %v", unitID, err)
			}
		}
	}
	return nil
}

func (r *ProtocolRuntime) IsRunning(unitID, fingerprint string) bool {
	status, ok := r.Status(unitID)
	if !ok || !status.Healthy {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	return ok && unit.fingerprint == fingerprint
}

func (r *ProtocolRuntime) Status(unitID string) (RuntimeStatus, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	if !ok {
		return RuntimeStatus{}, false
	}
	status := RuntimeStatus{
		Healthy:          len(unit.instances) > 0,
		Readiness:        "ready",
		DependencyIssues: append([]string(nil), unit.dependencyIssues...),
		StartedAt:        unit.startedAt,
		RestartCount:     unit.restarts,
	}
	seenPIDs := map[int]struct{}{}
	for _, instance := range unit.instances {
		if instance.PID > 0 {
			if _, duplicate := seenPIDs[instance.PID]; !duplicate {
				seenPIDs[instance.PID] = struct{}{}
				status.PIDs = append(status.PIDs, instance.PID)
			}
		}
		if !instance.Alive() {
			status.Healthy = false
		}
	}
	sort.Ints(status.PIDs)
	if len(status.DependencyIssues) > 0 {
		status.Readiness = "degraded"
	}
	return status, true
}

func (r *ProtocolRuntime) Events() <-chan RuntimeEvent { return r.events }

func (r *ProtocolRuntime) monitor(unitID string, generation uint64, instance *protocolbus.PluginInstance) {
	<-instance.Done()
	r.mu.Lock()
	unit, ok := r.units[unitID]
	if !ok || unit.generation != generation || unit.notified {
		r.mu.Unlock()
		return
	}
	unit.notified = true
	event := RuntimeEvent{
		UnitID:      unitID,
		Fingerprint: unit.fingerprint,
		Type:        "instance_exited",
		Message:     fmt.Sprint(instance.Err()),
		OccurredAt:  time.Now().UTC(),
	}
	r.mu.Unlock()
	select {
	case r.events <- event:
	default:
		if r.Logf != nil {
			r.Logf("运行时事件队列已满，丢弃 unit=%s type=%s", event.UnitID, event.Type)
		}
	}
}

func (r *ProtocolRuntime) monitorDependencies(unitID string, generation uint64) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		r.mu.RLock()
		unit, ok := r.units[unitID]
		if !ok || unit.generation != generation {
			r.mu.RUnlock()
			return
		}
		plugins := append([]InstalledPlugin(nil), unit.plugins...)
		r.mu.RUnlock()
		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		degraded, err := validateRuntimeRequirements(ctx, plugins, r.router, 150*time.Millisecond)
		cancel()
		if err != nil {
			if r.Logf != nil {
				r.Logf("unit %s 依赖丢失，停止数据面: %v", unitID, err)
			}
			_ = r.Stop(context.Background(), unitID)
			select {
			case r.events <- RuntimeEvent{UnitID: unitID, Fingerprint: unit.fingerprint, Type: "dependency_lost", Message: err.Error(), OccurredAt: time.Now().UTC()}:
			default:
			}
			return
		}
		r.mu.Lock()
		if current, exists := r.units[unitID]; exists && current.generation == generation {
			current.dependencyIssues = degraded
		}
		r.mu.Unlock()
	}
}

func (r *ProtocolRuntime) monitorLeadership(unitID string, generation uint64, leadership *controlplane.Leadership) {
	var err error
	select {
	case err = <-leadership.Lost():
		if err == nil {
			return
		}
	case <-leadership.Done():
		return
	}
	r.mu.RLock()
	unit, current := r.units[unitID]
	valid := current && unit.generation == generation && containsLeadership(unit.leaderships, leadership)
	r.mu.RUnlock()
	if !valid {
		return
	}
	if r.Logf != nil {
		r.Logf("unit %s 失去 leader fencing，立即停止数据面: %v", unitID, err)
	}
	select {
	case r.events <- RuntimeEvent{UnitID: unitID, Fingerprint: unit.fingerprint, Type: "leadership_lost", Message: err.Error(), OccurredAt: time.Now().UTC()}:
	default:
	}
	_ = r.Stop(context.Background(), unitID)
}

func containsLeadership(all []*controlplane.Leadership, target *controlplane.Leadership) bool {
	for _, leadership := range all {
		if leadership == target {
			return true
		}
	}
	return false
}

func (r *ProtocolRuntime) UnitIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.units))
	for id := range r.units {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Host 暴露只读宿主句柄，供内核服务层和端到端测试调用当前 unit 的贡献。
func (r *ProtocolRuntime) Host(unitID string) (*protocolbus.Host, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	unit, ok := r.units[unitID]
	if !ok {
		return nil, false
	}
	return unit.host, true
}

func (r *ProtocolRuntime) Close() error {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	units := r.units
	r.units = map[string]*runningUnit{}
	r.mu.Unlock()
	for _, unit := range units {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		closeRegistrations(ctx, unit.registrations)
		_ = unit.host.Drain(ctx)
		cancel()
		unit.host.Stop()
		for _, leadership := range unit.leaderships {
			_ = leadership.Close(context.Background())
		}
	}
	if r.RuntimePools != nil {
		return r.RuntimePools.Close()
	}
	return nil
}

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
