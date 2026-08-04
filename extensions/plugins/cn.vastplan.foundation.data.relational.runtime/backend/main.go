// Command databaseruntime starts the dedicated Database Runtime foundation
// plugin. A3 exposes management activation and stateless execution; signed
// instance-affine transactions use the host-issued Runtime audience.
package main

import (
	"log"
	"os"

	"cdsoft.com.cn/VastPlan/contracts/runtime/go/protocol"
	"cdsoft.com.cn/VastPlan/extensions/libraries/go/sharedstate"
	runtime "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/platformcontrolbootstrap"
	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.data.relational.runtime/databaseruntime/sqlsharedstate"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var startup struct {
		AllowInsecureTLS bool `json:"allowInsecureTLS"`
		MaxTransactions  int  `json:"maxTransactions"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		log.Fatalf("解析 Database Runtime 启动配置: %v", err)
	}
	registry, err := runtime.NewDefaultRegistry(runtime.ProviderSecurityPolicy{AllowInsecureTLS: startup.AllowInsecureTLS})
	if err != nil {
		log.Fatalf("注册 Database Provider: %v", err)
	}
	binding := sharedstate.NewBindingStore()
	bootstrapper, err := platformcontrolbootstrap.New(registry)
	if err != nil {
		log.Fatalf("初始化 Platform Control Bootstrapper: %v", err)
	}
	bootstrapService, err := platformcontrolbootstrap.NewService(bootstrapper, binding, os.Getenv("CREDENTIALS_DIRECTORY"))
	if err != nil {
		log.Fatalf("初始化 Platform Control Runtime Service: %v", err)
	}
	defer bootstrapService.Close()
	sharedStateService, err := sqlsharedstate.NewCapabilityService(binding)
	if err != nil {
		log.Fatalf("初始化 SQL Shared State Capability: %v", err)
	}
	service, err := runtime.NewService(registry, runtime.ServiceOptions{
		InstanceID: os.Getenv(protocol.RuntimeAudienceEnvKey), MaxTransactions: startup.MaxTransactions,
	})
	if err != nil {
		log.Fatalf("初始化 Database Runtime: %v", err)
	}
	defer service.Close()
	plugin := sdk.New(runtime.PluginID, runtime.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	plugin.Contribute(bootstrapService.Contribution())
	plugin.Contribute(sharedStateService.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Database Runtime 退出: %v", err)
	}
}
