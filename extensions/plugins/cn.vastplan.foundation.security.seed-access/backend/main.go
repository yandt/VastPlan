package main

import (
	"log"
	"os"

	seedaccess "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.seed-access/seedaccess"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	path := os.Getenv("VASTPLAN_SEED_ACCESS_STATE_FILE")
	authority, err := seedaccess.NewAuthority(seedaccess.FileStore{Path: path}, nil)
	if err != nil {
		log.Fatalf("初始化 Seed Authority: %v", err)
	}
	provider, err := seedaccess.NewProvider(authority)
	if err != nil {
		log.Fatalf("初始化 Seed Provider: %v", err)
	}
	plugin := sdk.New(seedaccess.PluginID, seedaccess.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(provider.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Seed Access 插件退出: %v", err)
	}
}
