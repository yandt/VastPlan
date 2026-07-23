// Command credentials 启动只暴露凭证元数据的 Vault Transit 凭证插件进程。
package main

import (
	"context"
	"log"
	"os"

	credentials "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentials"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var configuration credentials.Configuration
	if err := sdk.DecodeStartupConfiguration(&configuration); err != nil {
		log.Fatalf("读取凭证维护配置失败: %v", err)
	}
	maintenance, err := configuration.Policy()
	if err != nil {
		log.Fatalf("校验凭证维护配置失败: %v", err)
	}
	// Vault endpoint/key/state location 属于部署配置；token 只可从受控挂载文件读取。
	service, err := credentials.NewWithOptions(os.Getenv("VASTPLAN_CREDENTIALS_STATE_FILE"), credentials.VaultTransit{Address: os.Getenv("VASTPLAN_VAULT_ADDR"), Key: os.Getenv("VASTPLAN_VAULT_TRANSIT_KEY"), TokenFile: os.Getenv("VASTPLAN_VAULT_TOKEN_FILE")}, credentials.ServiceOptions{Maintenance: maintenance})
	if err != nil {
		log.Fatalf("初始化凭证服务失败: %v", err)
	}
	p := sdk.New(credentials.PluginID, credentials.PluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(credentials.Contribution(service))
	p.Contribute(credentials.MaterialLeaseContribution(service))
	maintenanceContext, stopMaintenance := context.WithCancel(context.Background())
	defer stopMaintenance()
	go service.RunMaintenance(maintenanceContext, func(err error) { log.Printf("托管凭证维护失败: %v", err) })
	if err := p.Serve(); err != nil {
		log.Fatalf("凭证插件退出: %v", err)
	}
}
