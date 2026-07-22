package main

import (
	"log"
	"os"

	broker "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication-broker/broker"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	service, err := broker.New(broker.FileCatalog{Path: os.Getenv("VASTPLAN_AUTHENTICATION_PROVIDER_CATALOG")}, broker.NewMemoryTransactionStore(4096))
	if err != nil {
		log.Fatalf("初始化 Authentication Broker: %v", err)
	}
	plugin := sdk.New(broker.PluginID, broker.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Authentication Broker 退出: %v", err)
	}
}
