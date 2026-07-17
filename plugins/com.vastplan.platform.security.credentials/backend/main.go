package main

import (
	"log"
	"os"

	credentials "cdsoft.com.cn/VastPlan/plugins/com.vastplan.platform.security.credentials/credentials"
	sdk "cdsoft.com.cn/VastPlan/sdk/go/plugin"
)

func main() {
	// Vault endpoint/key/state location 属于部署配置；token 只可从受控挂载文件读取。
	service, err := credentials.New(os.Getenv("VASTPLAN_CREDENTIALS_STATE_FILE"), credentials.VaultTransit{Address: os.Getenv("VASTPLAN_VAULT_ADDR"), Key: os.Getenv("VASTPLAN_VAULT_TRANSIT_KEY"), TokenFile: os.Getenv("VASTPLAN_VAULT_TOKEN_FILE")})
	if err != nil {
		log.Fatalf("初始化凭证服务失败: %v", err)
	}
	p := sdk.New(credentials.PluginID, credentials.PluginVersion, map[string]string{"backend": "^1.0"})
	p.Contribute(credentials.Contribution(service))
	if err := p.Serve(); err != nil {
		log.Fatalf("凭证插件退出: %v", err)
	}
}
