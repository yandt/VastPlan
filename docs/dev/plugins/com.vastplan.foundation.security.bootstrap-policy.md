# com.vastplan.foundation.security.bootstrap-policy

VastPlan 的首个正式基础插件，在系统全局设置服务启动前提供默认拒绝的权限基线。

## 命名空间

- `com.vastplan`：首方组织，由 Manifest 强制绑定 `publisher=vastplan`；
- `foundation`：不依赖 settings/credentials/database 的自举层；
- `security`：安全功能域；
- `bootstrap-policy`：组件。

命名解析器同时支持多级功能分类，例如 `platform.data.relational.connection-manager` 的分类路径是 `data.relational`；因此可按层级、一级功能域或完整子域分别制定策略。

## 贡献与运行模型

- 扩展点：`permission.checker`；
- 实例策略：`per-kernel`；
- 状态模型：`local-ephemeral`；
- 可见性/路由：`local + direct`；
- 依赖：无；
- 目标能力：`platform.settings`。

该插件同时提供独立进程入口、Backend 编译时静态适配和签名制品内的 dynamic-go `.so`，
权限实现只有一份。默认仍为独立进程；部署方通过插件级放置策略明确选择后，精确的
`0.1.0` 版本可静态或动态内嵌。代码声明必须与验签 Manifest 完全一致，dynamic-go 还要
通过首方身份、ABI、共同构建指纹与平台检查；三种形态都经过同一权限/钩子/Registry
和生命周期管道。详见 [ADR-0051](../decisions/ADR-0051-Backend混合插件运行与受控内嵌边界.md)。

插件注册最高优先级 `write-guard` 和最低优先级 `baseline`。`write-guard` 把未知操作视为写操作，仅允许 system 或直接登录管理员继续判定；`baseline` 允许 system/管理员访问，并允许已验证的 `foundation`/`platform` 首方插件执行 `get`、`list`、`changesSince`。其他身份默认拒绝。

## 安全边界

- 不读取 settings，避免自举循环；
- 不读取凭证或环境秘密；
- 插件调用者即使继承管理员 principal，也不能获得系统设置写权限；
- 命名空间必须先经过发布者绑定和制品信任链验证，不能单独作为身份凭据；
- 后续动态授权插件应使用更高于 baseline、低于 write-guard 的优先级。
- `allow-trusted` 只影响执行隔离，不自动允许内嵌；放置权限必须由部署方另行配置。

## 版本历史

- `0.1.0`：多级命名空间、自举写保护与首方基础层只读基线。
