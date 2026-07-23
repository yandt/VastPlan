package main

import (
	"errors"
	"flag"
	"strings"
	"time"

	"cdsoft.com.cn/VastPlan/core/shared/go/artifactassessment"
)

type options struct {
	packagePath, outputPath, reportPath, privateKeyPath                              string
	channel, policyID, providerID, keyID                                             string
	trivyPath, cacheDirectory, scannerVersion, databaseRevision, workRoot            string
	ttl, timeout                                                                     time.Duration
	allowedLicenses                                                                  string
	fullLicenseScan                                                                  bool
	printDatabaseRevision                                                            bool
	critical, high, medium, low, unknownVulnerability, deniedLicense, unknownLicense int64
}

func parseOptions() (options, error) {
	var value options
	flag.StringVar(&value.packagePath, "package", "", "待扫描插件 tar.gz")
	flag.StringVar(&value.outputPath, "output", "", "AdmissionRecord JSON 输出路径")
	flag.StringVar(&value.reportPath, "report", "", "Trivy 原始 JSON 归档路径")
	flag.StringVar(&value.privateKeyPath, "private-key", "", "0600 Ed25519 PKCS#8 PEM 或 base64 私钥")
	flag.StringVar(&value.channel, "channel", "testing", "制品 channel")
	flag.StringVar(&value.policyID, "policy", "", "安全策略 ID")
	flag.StringVar(&value.providerID, "provider", "", "Provider ID")
	flag.StringVar(&value.keyID, "key-id", "", "Provider key ID")
	flag.StringVar(&value.trivyPath, "trivy", "", "Trivy 规范绝对路径")
	flag.StringVar(&value.cacheDirectory, "trivy-cache", "", "已准备的 Trivy cache 目录（db 子目录必须是受控快照）")
	flag.StringVar(&value.scannerVersion, "scanner-version", "", "经部署验证的 Trivy 版本")
	flag.StringVar(&value.databaseRevision, "database-revision", "", "经更新任务验证的数据库 revision")
	flag.StringVar(&value.workRoot, "work-root", "", "仅属主可访问的临时工作根目录")
	flag.DurationVar(&value.ttl, "ttl", 168*time.Hour, "准入记录有效期")
	flag.DurationVar(&value.timeout, "timeout", 20*time.Minute, "单次扫描超时")
	flag.StringVar(&value.allowedLicenses, "allowed-licenses", "Apache-2.0,MIT,BSD-2-Clause,BSD-3-Clause,ISC", "允许的 SPDX license ID，逗号分隔")
	flag.BoolVar(&value.fullLicenseScan, "license-full", true, "扫描源码与许可证文本（成本更高）")
	flag.BoolVar(&value.printDatabaseRevision, "print-database-revision", false, "只计算 -trivy-cache 数据库快照摘要")
	flag.Int64Var(&value.critical, "max-critical", 0, "critical 漏洞上限；-1 表示不治理")
	flag.Int64Var(&value.high, "max-high", 0, "high 漏洞上限；-1 表示不治理")
	flag.Int64Var(&value.medium, "max-medium", -1, "medium 漏洞上限；-1 表示不治理")
	flag.Int64Var(&value.low, "max-low", -1, "low 漏洞上限；-1 表示不治理")
	flag.Int64Var(&value.unknownVulnerability, "max-unknown-vulnerability", 0, "未知漏洞上限；-1 表示不治理")
	flag.Int64Var(&value.deniedLicense, "max-denied-license", 0, "非白名单许可证上限；-1 表示不治理")
	flag.Int64Var(&value.unknownLicense, "max-unknown-license", 0, "未知许可证上限；-1 表示不治理")
	flag.Parse()
	if value.printDatabaseRevision {
		if strings.TrimSpace(value.cacheDirectory) == "" {
			return value, errors.New("计算数据库 revision 必须指定 -trivy-cache")
		}
		return value, nil
	}
	for _, required := range []string{value.packagePath, value.outputPath, value.reportPath, value.privateKeyPath, value.policyID, value.providerID, value.keyID, value.trivyPath, value.cacheDirectory, value.scannerVersion, value.databaseRevision, value.workRoot} {
		if strings.TrimSpace(required) == "" {
			return value, errors.New("缺少必需参数；使用 -h 查看说明")
		}
	}
	return value, nil
}

func (o options) maximum() (artifactassessment.MaximumFindings, error) {
	values := []*int64{&o.critical, &o.high, &o.medium, &o.low, &o.unknownVulnerability, &o.deniedLicense, &o.unknownLicense}
	for _, value := range values {
		if *value < -1 {
			return artifactassessment.MaximumFindings{}, errors.New("安全阈值不得小于 -1")
		}
	}
	return artifactassessment.MaximumFindings{
		Critical: threshold(o.critical), High: threshold(o.high), Medium: threshold(o.medium), Low: threshold(o.low),
		UnknownVulnerability: threshold(o.unknownVulnerability), DeniedLicense: threshold(o.deniedLicense), UnknownLicense: threshold(o.unknownLicense),
	}, nil
}

func threshold(value int64) *uint64 {
	if value < 0 {
		return nil
	}
	converted := uint64(value)
	return &converted
}

func (o options) licenses() []string {
	var result []string
	for _, item := range strings.Split(o.allowedLicenses, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}
