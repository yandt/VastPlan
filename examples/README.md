# VastPlan Examples

本目录保存可运行但不进入生产默认组合的示例：`plugins/` 只允许 `cn.vastplan.example.*` 插件，`deploy/` 保存与它们配套的开发 Profile、Application Composition 和派生 Deployment。

示例仍需通过 Manifest、签名制品、testing/workspace 仓库和真实 Runtime 链路；生产构建、Seed 与普通 Portal 默认拒绝它们。纯故障夹具继续位于 `engineering/e2e/fixtures/plugins/`。
