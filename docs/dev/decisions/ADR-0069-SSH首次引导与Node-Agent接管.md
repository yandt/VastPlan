# ADR-0069 Linux SSH 首次引导与 Node Agent 接管

- 状态：已采纳
- 日期：2026-07-18
- 关联：[ADR-0010](ADR-0010-插件服务与部署编排.md)、[ADR-0024](ADR-0024-单节点自动装配与回滚语义.md)、[ADR-0027](ADR-0027-NATS生产安全与最小权限.md)、[ADR-0044](ADR-0044-全局依赖编排与本地自治启动管理.md)、[ADR-0068](ADR-0068-分布式平台管理中心与强类型BFF.md)

## 背景

企业环境需要从平台管理中心配置 Backend 服务、插件组合和副本数，并把新 Linux 主机接入集群。目标环境当前没有统一的 Ansible、容器或 Kubernetes 基线，但普遍提供受控 SSH 与 systemd。

若 Portal 长期通过 SSH 管理进程，会形成高权限中心、放大凭证泄露面，并绕过现有 Deployment v2、Controller、Node Agent、能力租约和实际态收敛。若把任意 Shell 交给部署插件，则第三方适配器可以直接取得主机控制权，破坏内核信任边界。

## 决策

1. SSH 只用于首次引导。它安装经过 SHA-256 锁定、已在企业制品准入环节验证来源证明的 Backend Kernel，写入节点专属 NATS/transport 身份，并由 systemd 启动 Node Agent。
2. systemd 激活后，Node Agent 通过 mTLS、NKey 和 addressing 传输身份登记节点租约；后续插件下载、服务组合、升级、回滚和副本收敛全部复用控制面，不再使用 SSH。
3. 首个生产适配器固定为 `linux + ssh + systemd`。内核只暴露强类型引导请求，不暴露任意命令、脚本或可由浏览器指定的远端路径。
4. SSH 必须使用预登记 `known_hosts`，禁止 TOFU、`InsecureIgnoreHostKey`、密码认证和 SSH agent 转发；引导账户必须具备无交互执行唯一固定命令 `sudo -n /bin/sh -s --` 的权限。
5. SSH identity 与引导输入文件必须仅属主可访问。节点秘密只经 SSH stdin 进入远端，落盘到 `/etc/vastplan/secrets`，由 root 持有、`vastplan` 组只读；Portal、Deployment v2、日志和返回值都不得承载秘密。
6. systemd unit 固定使用专用无登录用户、`NoNewPrivileges`、只读系统目录和独立可写状态目录。第三方插件策略默认为 `require-isolation`，默认插件放置为独立进程。
7. 在线服务配置继续由 Platform Profile、Application Composition、Resolver 和 Deployment v2 表达。用户配置 `service unit + plugins + replicas + placement`，Controller 才能生成 Assignment；浏览器不能直接编辑或提交节点级 Assignment。
8. 未来 Docker、Kubernetes、云主机和 Ansible 通过部署适配器扩展，但适配器只能生成受 Schema 约束的类型化计划；制品验证、凭证使用、审批、审计和执行强制点仍属于可信内核。

## 备选方案

### Portal 直接长期 SSH

实现最少，但所有变更都依赖 SSH 可用性，无法复用节点自治、租约、灰度和故障接管，并让 Portal 持续持有主机高权限；拒绝。

### 外部 Ansible/Kubernetes 作为唯一真相源

成熟环境中可作为部署适配器，但当前企业环境没有统一基线，而且会把插件组合与 VastPlan 实际态拆成两套控制面；首期不采用。

### SSH 首次引导，Node Agent 长期接管

一次性解决主机纳管，同时复用已完成的声明式服务编排与集群能力；作为首期方案。

## 影响

- 正面：SSH 故障不会影响已纳管节点；服务副本与插件组合保持同一真相源；未来部署适配器可替换而不改变上层配置。
- 代价：首次接入前必须准备企业内核下载地址、SHA-256、known_hosts、节点证书/NKey、transport 身份和制品信任材料。
- 边界：systemd 激活后的签名 Node Lease 就绪判定与拉式作业收敛已由 [ADR-0071](ADR-0071-签名Node-Lease与可信就绪判定.md) 实施；部署 Draft 与管理页面仍在后续阶段。
