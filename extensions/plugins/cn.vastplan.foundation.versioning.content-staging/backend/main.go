// Command contentstaging starts the trusted Version Content Staging plugin.
package main

import (
	"context"
	"log"

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
	go service.RunReclaimer(ctx, reclaimInterval)
	plugin := sdk.New(staging.PluginID, pluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
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
