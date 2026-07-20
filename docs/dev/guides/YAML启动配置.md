# YAML 启动配置

Backend 内核允许使用 YAML 编写本地 config-as-code 文件，但会在启动边界立即转为规范 JSON。Schema 校验、内容摘要、NATS 控制面与插件启动参数仍只使用 JSON。

## 启动与校验

```bash
backend-kernel validate \
  -kind desired-v1 \
  -file /etc/vastplan/desired/desired-state.yaml

backend-kernel reconcile \
  -startup-file /etc/vastplan/desired/desired-state.yaml \
  -repository /var/lib/vastplan/repository \
  -runtime-root /var/lib/vastplan/runtime/plugins \
  -actual-state /var/lib/vastplan/runtime/actual-state.json
```

`-startup-file` 是面向启动的名称；原有 `-desired` 完全兼容，二者不可同时指定。Platform Profile、Application Composition、Deployment 和 Portal 配置的所有 `Parse*File` / `validate -file` 入口同样接受 `.yaml`、`.yml` 和 `.json`。

## 拆分文件

`$include` 必须是对象的唯一字段，并替换该对象。包含文件以当前文件所在目录为基准；根文件所在目录是不可越过的边界。

```yaml
# /etc/vastplan/desired/desired-state.yaml
version: 1
revision: 12
metadata:
  $include: metadata.yaml
units:
  - $include: services/platform.yaml
  - $include: services/database.yaml
```

数组中的 include 若返回数组，会原地展开：

```yaml
# services/platform.yaml
- id: platform-settings
  kind: service
  enabled: true
  service_role: backend
  replicas: 1
  plugins:
    - id: cn.vastplan.platform.configuration.global-settings
      version: "0.2.0"
```

对象内部也可以递归拆分，例如将某个插件的非敏感启动参数放入独立文件：

```yaml
config:
  plugins:
    cn.vastplan.foundation.data.relational.runtime:
      $include: ../plugin-config/database-runtime.yaml
```

## 安全与语法限制

- include 只允许相对路径；最终路径必须留在根配置目录内，且文件必须是非符号链接普通文件。
- 最多 16 层、128 个文件；单文件最大 4 MiB；循环引用会失败。
- 不支持 YAML anchor/alias、merge key (`<<`)、重复 key、标签或自定义类型。
- 日期、版本、带前导零 ID、连接串等一律加引号，例如 `version: "0.2.0"`、`created: "2026-07-20"`。
- 数字只使用 JSON 数字语法；不要使用 `0x10`、`1_000`、`.inf` 或 `.nan`。
- `$include` 不做深度合并。需要拆分一个对象时，让该对象整体由 include 提供；需要拼接列表时，将 include 写成列表项。

不应把密钥、令牌或凭证明文写入 YAML。仍使用受控文件路径、环境白名单和托管 CredentialRef。
