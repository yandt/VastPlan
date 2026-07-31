// Command contentstaging starts the trusted Version Content Staging plugin.
package main

import (
	"context"
	"log"
	"time"

	pluginhostv1 "cdsoft.com.cn/VastPlan/contracts/generated/go/pluginhost/v1"
	staging "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.versioning.content-staging/contentstaging"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

var pluginVersion = staging.PluginVersion

func main() {
	var configuration staging.StartupConfiguration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("解析 Version Content Staging 启动配置: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	service, reclaimInterval, err := staging.BuildConfiguredService(ctx, configuration)
	if err != nil {
		log.Fatalf("初始化 Version Content Staging: %v", err)
	}
	var tickets *uploadTicketStore
	if configuration.DataPlane != nil {
		tickets = newUploadTicketStore(*configuration.DataPlane, service)
	}
	transport, err := startUploadTransport(configuration.DataPlane, service, tickets)
	if err != nil {
		log.Fatalf("初始化 Content Upload 数据面: %v", err)
	}
	registrar := &uploadLeaseRegistrar{configuration: configuration.DataPlane}
	service.SetControlObserver(registrar.ensure)
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		_ = transport.Shutdown(shutdownCtx)
	}()
	go service.RunReclaimer(ctx, reclaimInterval)
	plugin := sdk.New(staging.PluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	plugin.Contribute(service.ContentReferenceContribution())
	plugin.Contribute(uploadDataPlaneContribution(tickets))
	plugin.OnLifecycle(func(_ context.Context, lifecycle *pluginhostv1.Lifecycle) error {
		if lifecycle.GetOp() == pluginhostv1.Lifecycle_OP_SHUTDOWN {
			cancel()
		}
		return nil
	})
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Version Content Staging 退出: %v", err)
	}
}
