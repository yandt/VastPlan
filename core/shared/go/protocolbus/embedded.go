package protocolbus

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"google.golang.org/protobuf/proto"

	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	contractv1 "cdsoft.com.cn/VastPlan/core/shared/go/contract/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/errorcode"
	"cdsoft.com.cn/VastPlan/core/shared/go/extpoint"
	pluginhostv1 "cdsoft.com.cn/VastPlan/core/shared/go/pluginhost/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/registry"
)

// EmbeddedHost 是内嵌处理器唯一允许使用的回调入口。实现会忽略处理器传入的
// Caller/Principal，以宿主保存的原始调用上下文重新签发插件身份，避免同进程代码
// 因可直接构造 Go 对象而获得比独立进程更宽的权限。
type EmbeddedHost interface {
	Call(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

type EmbeddedHandler func(context.Context, EmbeddedHost, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)

type EmbeddedContribution struct {
	ExtensionPoint string
	ID             string
	Priority       int32
	Descriptor     []byte
	Handlers       map[string]EmbeddedHandler
}

// EmbeddedLifecycle 与进程协议使用同一套生命周期操作。migration 只在迁移阶段非空。
type EmbeddedLifecycle func(context.Context, pluginhostv1.Lifecycle_Op, *MigrationCommand) error

// EmbeddedPlugin 是 dynamic-go 模块返回的进程内定义。它不是插件清单的替代品；
// LaunchEmbeddedWithPolicy 仍会逐项核对已经验签的 LaunchPolicy。
type EmbeddedPlugin struct {
	ID            string
	Version       string
	Contributions []EmbeddedContribution
	Lifecycle     EmbeddedLifecycle
}

const (
	// DynamicGoABIV1 是 Go .so 与 Backend 之间的窄入口版本。数据面仍使用
	// EmbeddedPlugin/Host.Invoke 契约，不向动态模块暴露 Host 内部实现。
	DynamicGoABIV1  = "vastplan.dynamic-go.v1"
	DynamicGoSymbol = "VastPlanDynamicGo"
)

// DynamicGoModule 是 DynamicGoSymbol 导出函数返回的不可变模块说明。
// BuildFingerprint 必须由同一发布构建为 Backend 和 .so 同时注入。
type DynamicGoModule struct {
	ABI              string
	BuildFingerprint string
	Plugin           EmbeddedPlugin
}

type DynamicGoEntrypoint func() DynamicGoModule

type embeddedInstance struct {
	id, pluginID, version string
	policy                LaunchPolicy
	routes                map[string]EmbeddedHandler
	lifecycleFn           EmbeddedLifecycle

	mu       sync.Mutex
	active   bool
	inflight int
	wg       sync.WaitGroup
	err      error
	done     chan struct{}
	close    sync.Once
}

func embeddedRouteKey(extensionPoint, id, operation string) string {
	return extensionPoint + "\x00" + id + "\x00" + operation
}

func (h *Host) LaunchEmbeddedWithPolicy(ctx context.Context, definition EmbeddedPlugin, policy LaunchPolicy) (*PluginInstance, error) {
	return h.LaunchEmbeddedKindWithPolicy(ctx, definition, policy, "embedded")
}

// LaunchEmbeddedKindWithPolicy 允许受信任执行驱动记录具体内嵌承载类型；kind
// 只用于诊断，身份与权限仍完全来自 LaunchPolicy。
func (h *Host) LaunchEmbeddedKindWithPolicy(ctx context.Context, definition EmbeddedPlugin, policy LaunchPolicy, kind string) (*PluginInstance, error) {
	policy = cloneLaunchPolicy(policy)
	if policy.BackgroundService || policy.AutonomousTenantID != "" {
		return nil, errors.New("后台服务不能以内嵌模式运行")
	}
	if definition.ID == "" || definition.Version == "" || policy.PluginID == "" || policy.Version == "" {
		return nil, errors.New("内嵌插件及启动策略必须包含身份和版本")
	}
	if definition.ID != policy.PluginID || definition.Version != policy.Version {
		return nil, fmt.Errorf("内嵌插件身份与验签清单不一致: 代码 %s@%s，清单 %s@%s",
			definition.ID, definition.Version, policy.PluginID, policy.Version)
	}
	if policy.Contributions == nil && len(definition.Contributions) != 0 {
		return nil, errors.New("内嵌插件必须由显式的验签贡献清单授权")
	}
	declared := make([]*pluginhostv1.Contribution, 0, len(definition.Contributions))
	routes := make(map[string]EmbeddedHandler)
	for _, contribution := range definition.Contributions {
		if contribution.ExtensionPoint == "" || contribution.ID == "" || len(contribution.Handlers) == 0 {
			return nil, fmt.Errorf("内嵌插件 %s 存在不完整贡献 %s/%s", definition.ID, contribution.ExtensionPoint, contribution.ID)
		}
		for operation, handler := range contribution.Handlers {
			if handler == nil {
				return nil, fmt.Errorf("内嵌贡献 %s/%s 的操作 %q 没有处理器", contribution.ExtensionPoint, contribution.ID, operation)
			}
			key := embeddedRouteKey(contribution.ExtensionPoint, contribution.ID, operation)
			if _, exists := routes[key]; exists {
				return nil, fmt.Errorf("内嵌处理器重复: %s/%s#%s", contribution.ExtensionPoint, contribution.ID, operation)
			}
			routes[key] = handler
		}
		declared = append(declared, &pluginhostv1.Contribution{
			ExtensionPoint: contribution.ExtensionPoint, Id: contribution.ID,
			Priority: contribution.Priority, DescriptorJson: append([]byte(nil), contribution.Descriptor...),
		})
	}
	if err := validateDeclaredContributions(policy.Contributions, declared, true); err != nil {
		return nil, err
	}
	instance := &embeddedInstance{
		id: "embedded-" + randomHex(12), pluginID: definition.ID, version: definition.Version,
		policy: policy, routes: routes, lifecycleFn: definition.Lifecycle, done: make(chan struct{}),
	}

	h.mu.Lock()
	if h.stopped.Load() {
		h.mu.Unlock()
		return nil, errors.New("宿主已经停止")
	}
	if h.byPlugin[definition.ID] != nil || h.embeddedByPlugin[definition.ID] != nil {
		h.mu.Unlock()
		return nil, fmt.Errorf("插件 %s 已有运行实例", definition.ID)
	}
	h.embedded[instance.id] = instance
	h.embeddedByPlugin[definition.ID] = instance
	h.mu.Unlock()

	registered := false
	defer func() {
		if !registered {
			h.detachEmbedded(instance)
		}
	}()
	for _, contribution := range declared {
		if err := validateAndRegisterEmbedded(h, instance, contribution); err != nil {
			return nil, err
		}
	}
	if err := instance.lifecycle(ctx, pluginhostv1.Lifecycle_OP_ACTIVATE, nil); err != nil {
		return nil, fmt.Errorf("激活内嵌插件 %s: %w", definition.ID, err)
	}
	instance.mu.Lock()
	instance.active = true
	instance.mu.Unlock()
	registered = true
	h.Logf("内嵌插件已激活 %s@%s", definition.ID, definition.Version)
	return &PluginInstance{PluginID: definition.ID, Version: definition.Version, SessionID: instance.id,
		RuntimeAudience: launchRuntimeAudience(instance.policy), runtimeKind: kind, embedded: instance}, nil
}

func validateAndRegisterEmbedded(h *Host, instance *embeddedInstance, contribution *pluginhostv1.Contribution) error {
	if err := pluginv1.ValidateDescriptor(contribution.ExtensionPoint, contribution.DescriptorJson); err != nil {
		return err
	}
	return h.Registry.Register(registry.Contribution{
		ExtensionPoint: contribution.ExtensionPoint, ID: contribution.Id, PluginID: instance.pluginID,
		Priority: int(contribution.Priority), Descriptor: contribution.DescriptorJson,
	})
}

func (i *embeddedInstance) begin() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if !i.active {
		return errors.New("内嵌插件未激活")
	}
	i.inflight++
	i.wg.Add(1)
	return nil
}

func (i *embeddedInstance) end() {
	i.mu.Lock()
	i.inflight--
	i.mu.Unlock()
	i.wg.Done()
}

func (i *embeddedInstance) invoke(ctx context.Context, host *Host, target *contractv1.CallTarget,
	callCtx *contractv1.CallContext, payload []byte) (response *pluginhostv1.InvokeResponse, invokeErr error) {
	if err := i.begin(); err != nil {
		return errorResponse(errorcode.PluginInactive, err.Error(), true), nil
	}
	defer i.end()
	defer func() {
		if recovered := recover(); recovered != nil {
			terminal := fmt.Errorf("内嵌插件 %s 处理器 panic: %v", i.pluginID, recovered)
			host.failEmbedded(i, terminal)
			response = errorResponse(errorcode.PluginHandlerError, terminal.Error(), false)
			invokeErr = nil
		}
	}()
	operation := target.GetOperation()
	handler := i.routes[embeddedRouteKey(target.ExtensionPoint, target.Capability, operation)]
	if handler == nil {
		handler = i.routes[embeddedRouteKey(target.ExtensionPoint, target.Capability, "")]
	}
	if handler == nil {
		return errorResponse(errorcode.CapabilityNotFound,
			fmt.Sprintf("未实现 %s/%s 的操作 %q", target.ExtensionPoint, target.Capability, operation), false), nil
	}
	base, err := projectContextForPlugin(callCtx, target, i.policy)
	if err != nil {
		return errorResponse(errorcode.PermissionDenied, err.Error(), false), nil
	}
	callback := &embeddedHostCall{host: host, instance: i, invokeCtx: ctx, base: base, live: true}
	// dynamic-go 与独立进程得到同样的调用快照语义。处理器可修改自己的副本，
	// 但不能污染宿主可信基线、后置钩子或同一调用中的回调鉴权。
	handlerContext := proto.Clone(base).(*contractv1.CallContext)
	result, out, err := handler(ctx, callback, handlerContext, payload)
	callback.mu.Lock()
	callback.live = false
	callback.mu.Unlock()
	if err != nil {
		return errorResponse(errorcode.PluginHandlerError, err.Error(), true), nil
	}
	if result == nil {
		return errorResponse(errorcode.RemoteInvalidResponse, "内嵌处理器返回空 CallResult", false), nil
	}
	if !host.limits().PayloadAllowed(out) {
		return errorResponse(errorcode.PayloadTooLarge,
			fmt.Sprintf("响应 payload 为 %d bytes，超过上限 %d bytes", len(out), host.limits().MaxPayloadBytes), false), nil
	}
	return &pluginhostv1.InvokeResponse{Result: result, Payload: out}, nil
}

type embeddedHostCall struct {
	host      *Host
	instance  *embeddedInstance
	invokeCtx context.Context
	base      *contractv1.CallContext
	mu        sync.Mutex
	live      bool
}

func (c *embeddedHostCall) Call(_ context.Context, target *contractv1.CallTarget, _ *contractv1.CallContext,
	payload []byte) (*contractv1.CallResult, []byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.live {
		return nil, nil, errors.New("内嵌宿主回调只在当前处理器调用期间有效")
	}
	if target == nil {
		return nil, nil, errors.New("内嵌宿主回调目标不能为空")
	}
	if target.ExtensionPoint == extpoint.KernelService && !kernelServiceAllowed(c.instance.policy, target.Capability) {
		return nil, nil, errors.New("插件未在签名清单中声明该内核服务")
	}
	authenticated := proto.Clone(c.base).(*contractv1.CallContext)
	authenticated.Caller = &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_PLUGIN, Id: c.instance.pluginID}
	invokeCtx := c.invokeCtx
	c.host.mu.RLock()
	_, localKernelService := c.host.services[target.Capability]
	c.host.mu.RUnlock()
	if target.ExtensionPoint == extpoint.KernelService && localKernelService {
		if trustedCtx, identityErr := withLaunchRuntimeIdentity(invokeCtx, c.instance.policy); identityErr == nil {
			invokeCtx = trustedCtx
		}
	}
	response, err := c.host.Invoke(invokeCtx, target, authenticated, payload)
	if err != nil {
		return nil, nil, err
	}
	if response == nil || response.Result == nil {
		return nil, nil, errors.New("宿主回调返回空结果")
	}
	return response.Result, response.Payload, nil
}

func (i *embeddedInstance) lifecycle(ctx context.Context, op pluginhostv1.Lifecycle_Op, migration *MigrationCommand) (err error) {
	if i.lifecycleFn == nil {
		if migration != nil {
			return errors.New("内嵌插件未实现状态迁移处理器")
		}
		return nil
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("内嵌插件生命周期 panic: %v", recovered)
		}
	}()
	return i.lifecycleFn(ctx, op, migration)
}

func (i *embeddedInstance) drain(ctx context.Context) error {
	i.mu.Lock()
	i.active = false
	i.mu.Unlock()
	if err := i.lifecycle(ctx, pluginhostv1.Lifecycle_OP_DRAIN, nil); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() { i.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (i *embeddedInstance) terminate(err error) {
	i.mu.Lock()
	i.active = false
	if i.err == nil {
		i.err = err
	}
	i.mu.Unlock()
	i.close.Do(func() { close(i.done) })
}

func (i *embeddedInstance) terminalError() error {
	select {
	case <-i.done:
		i.mu.Lock()
		defer i.mu.Unlock()
		return i.err
	default:
		return nil
	}
}

func (h *Host) failEmbedded(instance *embeddedInstance, err error) {
	h.detachEmbedded(instance)
	instance.terminate(err)
	h.Logf("内嵌插件已摘除 %s@%s: %v", instance.pluginID, instance.version, err)
}

func (h *Host) detachEmbedded(instance *embeddedInstance) {
	h.Registry.UnregisterPlugin(instance.pluginID)
	h.mu.Lock()
	if h.embedded[instance.id] == instance {
		delete(h.embedded, instance.id)
	}
	if h.embeddedByPlugin[instance.pluginID] == instance {
		delete(h.embeddedByPlugin, instance.pluginID)
	}
	h.mu.Unlock()
}

func (h *Host) closeEmbedded(instance *embeddedInstance) error {
	h.mu.RLock()
	owned := h.embedded[instance.id] == instance
	h.mu.RUnlock()
	if !owned {
		return nil
	}
	instance.mu.Lock()
	instance.active = false
	instance.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), h.limits().DrainTimeout)
	defer cancel()
	drainErr := instance.drain(ctx)
	lifecycleErr := instance.lifecycle(ctx, pluginhostv1.Lifecycle_OP_SHUTDOWN, nil)
	h.detachEmbedded(instance)
	instance.terminate(errors.New("宿主主动关闭"))
	return errors.Join(drainErr, lifecycleErr)
}
