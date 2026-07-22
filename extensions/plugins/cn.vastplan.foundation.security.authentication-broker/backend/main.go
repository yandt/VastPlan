package main

import (
	"log"
	"os"

	broker "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.foundation.security.authentication-broker/broker"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	stateStore := &broker.FileManagementStore{Path: os.Getenv("VASTPLAN_AUTHENTICATION_PROVIDER_STATE")}
	assertions, err := broker.LoadAssertionKey(os.Getenv("VASTPLAN_AUTHENTICATION_ASSERTION_KEY_FILE"))
	if err != nil {
		log.Fatalf("加载 Authentication Assertion key: %v", err)
	}
	service, err := broker.New(broker.StateCatalog{Store: stateStore}, broker.NewMemoryTransactionStore(4096), assertions)
	if err != nil {
		log.Fatalf("初始化 Authentication Broker: %v", err)
	}
	plugin := sdk.New(broker.PluginID, broker.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	management, err := broker.NewManagementService(stateStore, assertions)
	if err != nil {
		log.Fatalf("初始化 Authentication Provider Management: %v", err)
	}
	plugin.Contribute(management.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Authentication Broker 退出: %v", err)
	}
}
