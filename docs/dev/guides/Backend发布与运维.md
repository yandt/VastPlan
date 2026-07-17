# Backend 发布与运维

本文是 Backend Kernel 正式制品的可执行发布、升级、回滚、配置迁移和诊断手册。发布设计以 [ADR-0038](../decisions/ADR-0038-Backend可复现发布与运维交付.md) 为准；1.0 是否允许发版以《[Backend 内核 1.0 封板指南](Backend内核1.0封板.md)》为准。

## 1. 发布前门禁

发布负责人先确认目标提交位于 `main`，工作区干净，并执行：

```bash
./tools/test.sh --e2e
go test -race ./...
go vet ./...
./tools/benchmark.sh
./tools/verify-release.sh
```

还必须取得冻结发布候选提交的 24 小时 `Backend Kernel Soak` 成功报告。短时 smoke、工作流仍在运行或其他源码提交的报告都不能替代。报告不仅要使 job 成功，还必须通过 `tools/soakreport` 对 commit、时长、真实调用/重启和资源收敛的复验。

`./tools/verify-release.sh` 会执行两次独立构建并逐字节比较，用正式二进制预检仓库的 DesiredState v1 和 Deployment v2 样例，再验证 CycloneDX SBOM 可确定性重建。

soak 成功后，把被测 SHA 登记为推广证据。此后到标签提交只允许修改 `VERSION`、`SOAKED_COMMIT`、`CHANGELOG.md` 和封板验收状态；任何源码、工作流、Schema 或测试变化都必须重新运行 24 小时 soak：

```bash
soaked_commit="<24h-soak-成功运行的完整提交SHA>"
printf '%s\n' "$soaked_commit" > kernels/backend/SOAKED_COMMIT
```

只有《封板指南》的所有行均完成后，才能在同一推广提交中把 `kernels/backend/VERSION` 更新为目标版本并补充 `CHANGELOG.md`。提交并推送后创建精确标签：

```bash
version="$(tr -d '[:space:]' < kernels/backend/VERSION)"
git tag -a "backend-v${version}" -m "Backend Kernel ${version}"
git push origin "backend-v${version}"
```

标签触发 `Backend Kernel Release`。工作流必须生成四个平台二进制、逐制品 CycloneDX SBOM、`SHA256SUMS`、构建来源证明和 SBOM 证明；任何矩阵项失败都不得手工补建同名 Release。

Release 首先检查：标签与 `VERSION` 一致、标签提交属于 `main`、`SOAKED_COMMIT` 是其祖先、两者间只有推广白名单文件变化，并从 Actions 下载该 SHA 的合格 24 小时报告。该检查不能手工跳过。

## 2. 制品接收与验证

以下以 Linux amd64 为例，`REPOSITORY` 应使用实际 GitHub 仓库：

```bash
export REPOSITORY=yandt/VastPlan
export VERSION=1.0.0
gh release download "backend-v${VERSION}" --repo "$REPOSITORY" --dir release
cd release
sha256sum --check SHA256SUMS
gh attestation verify backend-kernel-linux-amd64 \
  --repo "$REPOSITORY" \
  --signer-workflow yandt/VastPlan/.github/workflows/backend-release.yml \
  --source-ref "refs/tags/backend-v${VERSION}"
gh attestation verify backend-kernel-linux-amd64 \
  --repo "$REPOSITORY" \
  --signer-workflow yandt/VastPlan/.github/workflows/backend-release.yml \
  --source-ref "refs/tags/backend-v${VERSION}" \
  --predicate-type https://cyclonedx.org/bom
chmod 0755 backend-kernel-linux-amd64
./backend-kernel-linux-amd64 version --json
```

`gh attestation verify` 默认要求 SLSA provenance；CycloneDX SBOM 必须显式指定 `https://cyclonedx.org/bom` predicate。气隙部署必须在制品入区前完成在线证明验证，将验证记录、二进制、SBOM 和 `SHA256SUMS` 一起带入；入区后重新校验 SHA-256。

## 3. 配置和状态迁移预检

预检直接使用待上线二进制，确保检查器和运行时不会漂移：

```bash
./backend-kernel-linux-amd64 validate \
  -kind desired-v1 -file /etc/vastplan/desired-state.json

./backend-kernel-linux-amd64 validate \
  -kind deployment-v2 -file /etc/vastplan/deployment.json

./backend-kernel-linux-amd64 validate \
  -kind actual-state -file /var/lib/vastplan/actual-state.json
```

- 本地 Node Agent 使用 DesiredState v1；集群 Controller 输入使用 Deployment v2。两者不会在 Backend 1.0 发布时自动互相转换。
- actual-state v1 在预检中只于内存转换为 v2，不修改源文件。正式进程加载后，下一次成功保存只写 v2。
- 未知 actual-state 版本、未知 v1 status、Schema 错误或同 revision 内容冲突必须先处理，不得跳过预检启动。
- 所有待运行插件的 `engines.backend` 必须显式包含目标 Backend 1.0；`^0.1` 不会被 1.0 宿主默认为兼容。
- 升级插件状态格式时，候选插件必须声明 `state.backend.migration`；内核不会猜测、复制或修改插件私有数据。

### 3.1 插件运行形态

Backend 默认用独立进程运行所有插件。内嵌是独立的部署策略轴，不能用发布者信任
策略代替：

```text
-plugin-placement-default=process-only
-publisher-plugin-placements=vastplan=prefer-embedded
-plugin-placements=com.vastplan.foundation.security.bootstrap-policy=require-embedded
# 明确要求从已签名 .so 加载时：
-plugin-placements=com.vastplan.foundation.security.bootstrap-policy=require-dynamic-go
```

规则优先级为“插件 > 发布者 > 全局”，可选值为 `process-only`、
`prefer-embedded`、`require-embedded`、`prefer-dynamic-go`、`require-dynamic-go`。
生产建议保持全局 `process-only`，逐插件启用；发布者级规则适合已经完成统一评审的封闭
发布物。只有 `publisher=vastplan + com.vastplan.*` 首方硬身份、精确 ID/版本、验签贡献
清单和 `trusted-process` 隔离下限同时满足时才会内嵌。

`prefer-embedded` 按静态目录、dynamic-go、进程回退；dynamic-go 专用值跳过静态目录。
代码定义与清单不一致属于发布漂移，直接拒绝。dynamic-go 只支持 Linux/FreeBSD/macOS
原生 `CGO_ENABLED=1` 共同构建，Backend、`.so` 与签名 Manifest 的构建指纹必须一致；
加载器会在 `plugin.Open` 前先校验签名指纹，再验证模块导出信息。标准库 plugin
不能卸载，所以升级必须滚动重启 Backend，不做同进程热替换。共同构建命令为：

```bash
OUT_DIR=bin/dynamic-go ./tools/build-dynamic-go.sh
```

其他平台或 `CGO_ENABLED=0` 的 Backend 保留进程/静态能力并拒绝 dynamic-go。具体安全与
故障边界见 [ADR-0051](../decisions/ADR-0051-Backend混合插件运行与受控内嵌边界.md)。

## 4. 升级

以下目录只作为标准布局示例；服务管理器可以替换，但必须保留“版本目录 + 原子切换 + 旧版本保留”语义：

```bash
export VERSION=1.0.0
export VASTPLAN_HOME=/opt/vastplan/backend
export VASTPLAN_STATE=/var/lib/vastplan
sudo install -d -m 0755 "$VASTPLAN_HOME/releases/$VERSION"
sudo install -m 0755 release/backend-kernel-linux-amd64 \
  "$VASTPLAN_HOME/releases/$VERSION/backend-kernel"
sudo install -d -m 0700 "$VASTPLAN_STATE/backups"
sudo cp -p "$VASTPLAN_STATE/actual-state.json" \
  "$VASTPLAN_STATE/backups/actual-state.before-$VERSION.json"
sudo cp -p /etc/vastplan/desired-state.json \
  "$VASTPLAN_STATE/backups/desired-state.before-$VERSION.json"
```

先在一个 canary 节点停止旧进程，创建同目录临时链接后原子替换：

```bash
sudo ln -s "releases/$VERSION/backend-kernel" "$VASTPLAN_HOME/current.next"
sudo mv -Tf "$VASTPLAN_HOME/current.next" "$VASTPLAN_HOME/current"
```

由现有服务管理器启动 `$VASTPLAN_HOME/current reconcile ...`。启动后必须检查：

1. `version --json` 返回目标版本；
2. health 为 healthy、readiness 为 ready；
3. actual-state 的 `applied_revision` 达到目标，所有目标 unit 为 `active`；
4. 候选状态为空，没有持续增加的 restart、pending、goroutine 或文件句柄；
5. 核心能力执行一组真实请求，日志中无新增稳定错误码。

canary 观察窗通过后再逐节点执行，不同时替换全部 Controller/Node Agent。控制器多副本升级期间保留至少一个健康 Leader；Node Agent 逐节点升级，等待前一节点重新 ready 后继续。

## 5. 配置回滚与二进制回滚

配置回滚必须发布备份快照本身，保留其原 revision；不要复制内容后沿用当前 revision。集群模式可使用控制面发布工具：

```bash
go run ./kernels/backend controlplane \
  -nats-url tls://nats.example.com:4222 \
  -nats-ca /etc/vastplan/pki/ca.crt \
  -nats-cert /etc/vastplan/pki/controller.crt \
  -nats-key /etc/vastplan/pki/controller.key \
  -nats-seed /secure/controller.seed \
  -repository /var/lib/vastplan/repository \
  -desired /var/lib/vastplan/backups/deployment.before-1.0.0.json
```

本地文件模式用备份文件原子替换 DesiredState，Node Agent 会接受较小 revision 并按内容指纹决定是否重启。

若需要回滚 Backend 二进制：

1. 停止当前 Backend，避免两个版本同时持有进程锁；
2. 先生成支持包；
3. 将 `current` 原子切回旧版本；
4. 恢复升级前的 actual-state 备份和旧配置；
5. 启动旧版本并验证 health、readiness、revision 和真实能力请求。

```bash
export PREVIOUS_VERSION=0.1.0
sudo ln -s "releases/$PREVIOUS_VERSION/backend-kernel" /opt/vastplan/backend/current.next
sudo mv -Tf /opt/vastplan/backend/current.next /opt/vastplan/backend/current
sudo cp -p /var/lib/vastplan/backups/actual-state.before-1.0.0.json \
  /var/lib/vastplan/actual-state.json
sudo cp -p /var/lib/vastplan/backups/desired-state.before-1.0.0.json \
  /etc/vastplan/desired-state.json
```

不要让旧 Backend 读取已写成未知新版本的 actual-state。插件业务状态迁移只允许在切换所有权前回滚；若候选已经取得所有权，应发布一个新的、显式兼容迁移版本，不能手工修改插件私有存储。

## 6. 诊断与支持包

先经部署适配器调用 `kernel.diagnostics` 并把 JSON 保存为仅 root 可读文件，再执行：

```bash
sudo install -d -m 0700 /var/lib/vastplan/support
sudo /opt/vastplan/backend/current support-bundle \
  -actual-state /var/lib/vastplan/actual-state.json \
  -diagnostics /run/vastplan/kernel-diagnostics.json \
  -output /var/lib/vastplan/support/backend-$(date -u +%Y%m%dT%H%M%SZ).tar.gz
```

支持包只含：

- 内核版本、Go 工具链、平台和当前二进制 SHA-256；
- unit phase、revision、restart、PID 和插件身份摘要；
- 错误是否存在及发生阶段，不含错误正文；
- 经二次敏感字段脱敏的实时诊断；
- 除 manifest 自身外的包内内容文件 SHA-256 清单。

发送前仍应在隔离环境解包人工复核。不要额外附加 DesiredState、环境变量、NKey seed、TLS 私钥、仓库令牌、插件私有数据或原始业务日志。

## 7. 发布失败处置

- 任一矩阵构建、SBOM 或 attestation 失败：删除失败的草稿/不完整 Release，修复后创建新提交和新版本；不得替换已发布标签指向。
- 校验和或证明不匹配：隔离制品并停止部署，按供应链事件处理；不要从其他节点复制“看起来一样”的二进制绕过验证。
- canary 不 ready 或状态不收敛：立即停止扩大部署，生成支持包并按第 5 节回滚。
- 24h soak 尚未成功：保持 `0.x`，不得创建 `backend-v1.0.0` 标签。
