package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"time"

	"github.com/nats-io/nats.go/jetstream"

	contractv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/contract/v1"
	"cdsoft.com.cn/VastPlan/contracts/runtime/go/extpoint"
	pluginv1 "cdsoft.com.cn/VastPlan/contracts/schemas/plugin/v1"
	"cdsoft.com.cn/VastPlan/core/shared/go/addressing"
	"cdsoft.com.cn/VastPlan/core/shared/go/controlplane"
	"cdsoft.com.cn/VastPlan/engineering/internal/pluginlibrarysource"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/platformadminapi"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/plugininstallation"
)

const developmentInstallationCaller = "platform-dev/installation-watch"

type capabilityInvoker interface {
	Invoke(context.Context, *contractv1.CallTarget, *contractv1.CallContext, []byte) (*contractv1.CallResult, []byte, error)
}

type developmentInstallationClient struct {
	invoker capabilityInvoker
	logf    func(string, ...any)
}

func (c developmentInstallationClient) ApplyInstallationIntent(ctx context.Context, intent pluginlibrarysource.InstallationIntent) error {
	bindings, err := c.listBindings(ctx)
	if err != nil {
		return err
	}
	matched := make([]platformadminapi.TestTargetBinding, 0)
	for _, binding := range bindings {
		if binding.Enabled && binding.Kind == platformadminapi.TestTargetBackend && binding.PluginID == intent.PluginID {
			matched = append(matched, binding)
		}
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].ID < matched[j].ID })
	for _, binding := range matched {
		if intent.Action == plugininstallation.ActionInstall && !binding.AllowInstall {
			continue
		}
		request := plugininstallation.PreviewRequest{
			Version:       plugininstallation.ProtocolVersion,
			Target:        plugininstallation.Target{Kernel: "backend", Deployment: binding.Deployment, UnitID: binding.UnitID},
			PortalTargets: append([]string(nil), binding.PortalTargets...),
			Change:        plugininstallation.Change{Action: intent.Action, PluginID: intent.PluginID},
		}
		if intent.Artifact != nil {
			request.Change.Requirement = &pluginv1.ArtifactRequirement{
				PluginID: intent.Artifact.PluginID, Constraint: "=" + intent.Artifact.Version, Channel: intent.Artifact.Channel,
			}
		}
		var candidate plugininstallation.Candidate
		if err := c.call(ctx, plugininstallation.DevelopmentApplyOperation, map[string]any{"installationPreview": request}, &candidate); err != nil {
			return fmt.Errorf("应用开发安装意图 binding=%s: %w", binding.ID, err)
		}
		if candidate.Status != plugininstallation.CandidateReady {
			return fmt.Errorf("开发安装候选未就绪 binding=%s candidate=%s status=%s", binding.ID, candidate.ID, candidate.Status)
		}
		c.logger()("开发安装意图已激活 binding=%s plugin=%s action=%s candidate=%s", binding.ID, intent.PluginID, intent.Action, candidate.ID)
	}
	return nil
}

func (c developmentInstallationClient) listBindings(ctx context.Context) ([]platformadminapi.TestTargetBinding, error) {
	var response struct {
		Items []platformadminapi.TestTargetBinding `json:"items"`
	}
	if err := c.call(ctx, "listTestTargetBindings", struct{}{}, &response); err != nil {
		return nil, fmt.Errorf("读取开发安装目标绑定: %w", err)
	}
	return response.Items, nil
}

func (c developmentInstallationClient) call(ctx context.Context, operation string, payload, output any) error {
	if c.invoker == nil {
		return errors.New("开发安装 capability invoker 未配置")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	logicalService, routingDomain := "platform.deployment", "platform"
	target := &contractv1.CallTarget{
		ExtensionPoint: extpoint.ToolPackage, Capability: platformadminapi.DeploymentCapability, Operation: &operation,
		LogicalService: &logicalService, RoutingDomain: &routingDomain,
	}
	call := &contractv1.CallContext{
		TenantId: "local", Scene: "development.plugin-watch",
		Caller: &contractv1.Caller{Kind: contractv1.CallerKind_CALLER_KIND_SYSTEM, Id: developmentInstallationCaller},
	}
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var result *contractv1.CallResult
	var response []byte
	for {
		result, response, err = c.invoker.Invoke(callCtx, target, call, raw)
		if err == nil || !errors.Is(err, addressing.ErrCapabilityNotFound) {
			break
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-callCtx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return callCtx.Err()
		case <-timer.C:
		}
	}
	if err != nil {
		return err
	}
	if result == nil || result.Status != contractv1.CallResult_STATUS_OK {
		if result != nil && result.Error != nil {
			return fmt.Errorf("%s: %s", result.Error.Code, result.Error.Message)
		}
		return errors.New("开发安装 capability 返回非成功状态")
	}
	if output == nil {
		return nil
	}
	if err := json.Unmarshal(response, output); err != nil {
		return fmt.Errorf("解析开发安装响应: %w", err)
	}
	return nil
}

func (c developmentInstallationClient) logger() func(string, ...any) {
	if c.logf != nil {
		return c.logf
	}
	return log.Printf
}

func (r *runtime) openDevelopmentInstallationClient() (developmentInstallationClient, func(), error) {
	nc, err := controlplane.Connect("nats://"+r.options.natsListen, "platform-dev-installation-watch", log.Printf)
	if err != nil {
		return developmentInstallationClient{}, nil, err
	}
	security, err := addressing.LoadTransportSecurity(
		filepath.Join(r.runDir, "secrets", platformDevTransportSeed),
		filepath.Join(r.runDir, "secrets", transportTrustDocument),
	)
	if err != nil {
		nc.Close()
		return developmentInstallationClient{}, nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		security.Close()
		nc.Close()
		return developmentInstallationClient{}, nil, err
	}
	directoryCtx, cancelDirectory := context.WithTimeout(context.Background(), 10*time.Second)
	directory, err := js.KeyValue(directoryCtx, controlplane.CapabilitiesBucket)
	cancelDirectory()
	if err != nil {
		security.Close()
		nc.Close()
		return developmentInstallationClient{}, nil, err
	}
	router, err := addressing.NewSecureRouter(nc, directory, "platform-dev", log.Printf, security)
	if err != nil {
		security.Close()
		nc.Close()
		return developmentInstallationClient{}, nil, err
	}
	close := func() {
		_ = router.Close()
		security.Close()
		_ = nc.Drain()
		nc.Close()
	}
	return developmentInstallationClient{invoker: router, logf: log.Printf}, close, nil
}

var _ pluginlibrarysource.InstallationIntentApplier = developmentInstallationClient{}
