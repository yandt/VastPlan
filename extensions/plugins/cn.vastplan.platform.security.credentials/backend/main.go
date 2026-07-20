// Command credentials 启动只暴露凭证元数据的 Vault Transit 凭证插件进程。
package main

import (
	"log"
	"os"

	credentials "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.security.credentials/credentials"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	// Vault endpoint/key/state location 属于部署配置；token 只可从受控挂载文件读取。
	service, err := credentials.New(os.Getenv("VASTPLAN_CREDENTIALS_STATE_FILE"), credentials.VaultTransit{Address: os.Getenv("VASTPLAN_VAULT_ADDR"), Key: os.Getenv("VASTPLAN_VAULT_TRANSIT_KEY"), TokenFile: os.Getenv("VASTPLAN_VAULT_TOKEN_FILE")})
	if err != nil {
		log.Fatalf("初始化凭证服务失败: %v", err)
	}
	p := sdk.New(credentials.PluginID, credentials.PluginVersion, map[string]string{"backend": "^0.1"})
	p.Contribute(credentials.Contribution(service))
	p.Contribute(credentials.MaterialLeaseContribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("凭证插件退出: %v", err)
	}
}
