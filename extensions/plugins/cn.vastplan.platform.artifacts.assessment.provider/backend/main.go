// Command artifactassessmentprovider starts the independent trusted scanner.
package main

import (
	"log"

	assessmentprovider "cdsoft.com.cn/VastPlan/extensions/plugins/cn.vastplan.platform.artifacts.assessment.provider/assessmentprovider"
	provider "cdsoft.com.cn/VastPlan/extensions/sdk/go/artifactassessmentprovider"
	sdk "cdsoft.com.cn/VastPlan/extensions/sdk/go/plugin"
)

func main() {
	var config assessmentprovider.Config
	if err := sdk.DecodeStartupConfiguration(&config); err != nil {
		log.Fatalf("读取 Assessment Provider 配置: %v", err)
	}
	engine, err := provider.NewTrivy(provider.TrivyConfig{
		Binary: config.TrivyBinary, CacheDirectory: config.TrivySnapshotDirectory, ScannerVersion: config.ScannerVersion,
		DatabaseRevision: config.DatabaseRevision, Timeout: config.Timeout(), AllowedLicenses: config.AllowedLicenses, FullLicenseScan: config.FullLicenseScan,
	})
	if err != nil {
		log.Fatalf("初始化 Trivy: %v", err)
	}
	service, err := assessmentprovider.New(config, engine, assessmentprovider.NewHTTPSDownloader())
	if err != nil {
		log.Fatalf("初始化 Assessment Provider: %v", err)
	}
	plugin := sdk.New(assessmentprovider.PluginID, assessmentprovider.PluginVersion, map[string]string{"backend": "^0.1"})
	plugin.Contribute(service.Contribution())
	if err := plugin.Serve(); err != nil {
		log.Fatalf("Assessment Provider 退出: %v", err)
	}
}
