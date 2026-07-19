# 平台管理访问策略

插件 ID：`cn.vastplan.foundation.security.platform-admin-access-policy`

该 foundation 插件以 `per-kernel + local-ephemeral + local + direct` 运行，在设置、凭证、数据库、制品和部署能力所在 unit 内执行最终授权。它不读取 settings，不依赖远端服务，也不向浏览器暴露能力。

策略只处理五个精确平台 capability，以及全局设置/凭证的 `kernel.config.get`、数据库连接插件的 `kernel.database.probe` 和 Deployment Manager 的 `kernel.node.bootstrap`、`kernel.node.readiness` 精确宿主回调。未知平台操作拒绝，其他业务能力弃权；全部策略缺失时宿主仍按零校验器 fail-closed。

角色、Edge 前置检查、settings 自举策略顺序和部署要求见《[平台管理中心](../architecture/平台管理中心.md)》与 [ADR-0068](../decisions/ADR-0068-分布式平台管理中心与强类型BFF.md)。
