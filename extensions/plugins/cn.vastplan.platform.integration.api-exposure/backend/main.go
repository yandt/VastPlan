// Command apiexposure 启动治理式 API Exposure 控制面插件。
package main

import (
	"log"

	"cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.integration.api-exposure/apiexposure"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var startup struct {
		StateFile           string `json:"stateFile"`
		GatewayCatalogFile  string `json:"gatewayCatalogFile"`
		ContractCatalogFile string `json:"contractCatalogFile"`
	}
	if err := sdk.DecodeStartupConfiguration(&startup); err != nil {
		log.Fatalf("读取 API Exposure 启动配置失败: %v", err)
	}
	catalog, err := apiexposure.LoadContractCatalogFile(startup.ContractCatalogFile)
	if err != nil {
		log.Fatalf("读取 API Contract Catalog 失败: %v", err)
	}
	service, err := apiexposure.New(startup.StateFile, startup.GatewayCatalogFile, catalog)
	if err != nil {
		log.Fatalf("初始化 API Exposure 服务失败: %v", err)
	}
	plugin := sdk.New(apiexposure.PluginID, apiexposure.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(apiexposure.Contribution(service))
	if err := plugin.Serve(); err != nil {
		log.Fatalf("API Exposure 插件退出: %v", err)
	}
}
