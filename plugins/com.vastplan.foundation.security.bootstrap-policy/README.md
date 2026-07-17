# VastPlan Bootstrap Policy

`com.vastplan.foundation.security.bootstrap-policy` 是 Backend 内核启动
`platform.settings` 之前使用的最小权限基线插件。它以本地、无状态、默认拒绝的方式
保护系统设置能力，不依赖 settings、credentials、database 或其他插件。

> 本 README 是插件开发与验证入口。权威架构决策见
> [ADR-0050](../../docs/dev/decisions/ADR-0050-首方插件多级命名空间与自举权限基线.md)，
> 完整插件说明见
> [插件文档](../../docs/dev/plugins/com.vastplan.foundation.security.bootstrap-policy.md)。

## 命名空间

插件 ID 遵循首方多级命名规则：

```text
com.vastplan.<layer>.<category...>.<component>
```

本插件可拆分为：

| 部分 | 值 | 含义 |
|---|---|---|
| 发布组织 | `com.vastplan` | 必须与 `publisher=vastplan` 绑定 |
| 层级 | `foundation` | 不依赖平台服务的自举基础层 |
| 功能分类 | `security` | 安全与授权功能域 |
| 组件 | `bootstrap-policy` | 系统启动期权限基线 |

命名空间只提供可信身份的功能分类，不能替代 Manifest 校验、制品签名或运行时授权。

## 运行模型

| 配置 | 值 | 说明 |
|---|---|---|
| 激活时机 | `onStartup` | 随 Backend 内核启动 |
| 实例策略 | `per-kernel` | 每个内核运行一个本地实例 |
| 状态模型 | `local-ephemeral` | 不保存业务状态 |
| 可见性 | `local` | 不发布为跨内核集群服务 |
| 路由 | `direct` | 内核本地直接调用 |
| 运行时依赖 | 无 | 避免 settings 权限检查形成自举循环 |

## 权限检查器

插件向 `permission.checker` 扩展点注册两个检查器：

1. `write-guard`，优先级 `1000000`：保护写操作。除 system 和直接登录的管理员用户外，其他调用者执行非只读或未知操作时立即拒绝。
2. `baseline`，优先级 `-1000000`：在其他策略均未给出结论时提供最低权限基线。

两个检查器都声明：

```json
"applies": { "target": "platform.settings" }
```

这表示宿主只在访问 `platform.settings` 时调用它们。该声明不是插件依赖，也不会让
bootstrap-policy 主动调用 settings；代码内部还会再次核对 capability，作为纵深防御。

在没有中间动态策略覆盖时，自举基线如下：

| 调用身份 | `get` / `list` / `changesSince` | 其他或未知操作 |
|---|---:|---:|
| system | 允许 | 允许 |
| 直接登录的管理员用户 | 允许 | 允许 |
| 已验证的 `foundation` / `platform` 首方插件 | 允许 | 拒绝 |
| 其他身份 | 拒绝 | 拒绝 |

插件调用者即使携带管理员 principal，也不能继承系统设置写权限，避免 confused-deputy
风险。后续动态策略可使用介于两个检查器之间的优先级细化读取权限，但不能绕过最高优先级写保护。

## 目录结构

```text
.
├── README.md
├── vastplan.plugin.json   # 身份、运行模型和贡献声明
└── backend
    ├── main.go            # 插件入口和贡献注册
    ├── policy.go          # 权限判定实现
    └── policy_test.go     # 权限矩阵单元测试
```

## 构建与测试

以下命令均从仓库根目录执行：

```bash
mkdir -p bin
go build \
  -o bin/com.vastplan.foundation.security.bootstrap-policy \
  ./plugins/com.vastplan.foundation.security.bootstrap-policy/backend

go test \
  ./plugins/com.vastplan.foundation.security.bootstrap-policy/backend \
  ./shared/go/pluginid \
  ./schemas/plugin/v1

go test -tags=e2e ./e2e \
  -run TestBootstrapPolicy_RealProcessEnforcesSettingsBaseline \
  -count=1
```

## 制品打包

使用仓库提供的打包工具生成安装制品：

```bash
go run ./tools/pluginpackage \
  -source plugins/com.vastplan.foundation.security.bootstrap-policy \
  -backend-bin bin/com.vastplan.foundation.security.bootstrap-policy \
  -out /tmp/com.vastplan.foundation.security.bootstrap-policy.tar.gz
```

打包工具会校验 Manifest 和后端入口，并按 Manifest 声明从仓库根目录注入
`LICENSE` 与 `NOTICE`。生产发布还必须经过制品摘要、签名、发布者证明和安装授权验证。

## 修改规则

- 新增只读操作时，同时更新 `settingsReadOperations`、权限矩阵测试和本文档。
- 修改优先级或调用身份规则时，同步更新 ADR-0050 和插件文档。
- 不允许读取 settings 或其他需要权限判定的服务来配置本插件，避免权限递归。
- 未知操作继续按写操作处理，保持 fail-closed。
