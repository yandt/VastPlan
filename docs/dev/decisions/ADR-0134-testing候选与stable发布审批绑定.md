# ADR-0134：testing 候选与 stable 发布审批绑定

状态：Accepted（2026-07-24）

## 背景

托管仓库原有 HTTPS 发布入口只要持有发布 token 且制品验签通过，就能直接写入任意 channel。Portal 若只保存一条普通“已批准”记录，最终上传仍可能换成另一份制品；若引入 staging 大对象，又会新增临时存储、配额、清理和崩溃恢复体系。

## 决策

stable 发布必须来自仓库中已经验签且处于 `active` 的精确 testing 制品：

1. 提交记录绑定 testing ref、目标 stable ref、包 SHA-256、publisher、key ID 和源证明摘要，并以独立 publication revision 做 CAS；
2. 批准必须由可信用户执行，且批准人与提交人不能相同；插件调用者或仅持有仓库发布 token 的 CI 不能代替人员批准；
3. stable HTTPS 上传仍走原有内核验签与内容校验，且在物理写入前必须命中一条 `Approved` 记录，最终证明中的 SHA、publisher、key ID 和目标 ref 必须完全一致；
4. 发布成功后原子推进为 `Published` 并记录最终证明摘要。若进程在对象提交后、状态推进前崩溃，启动恢复会以已验签 Catalog 重新收敛；批准不能被不同字节复用；
5. testing 发布保持原路径，Seed 自举仍与托管仓库分离。发布审批状态随 File Volume 迁移复制，观察期双写时两卷必须具有相同审批状态，否则冻结发布；
6. Portal 只读取证据摘要和审批轨迹，不接收原始包、签名私钥、信任文档、仓库 token、mount path 或 Provider endpoint。

## 取舍

该方案不需要 staging 大对象，复用测试发布闭环且把审批绑定到不可变内容。代价是 stable 制品必须先经过 testing 仓库，且最终发布仍由 CI/CLI 完成；Portal 的批准按钮本身不会上传大对象。未来若必须支持“首次即 stable”的外部供应商制品，应新增隔离 staging Provider，而不是放宽本 ADR 的绑定规则。

2026-07-24 实施补充：生产发布继续使用现有 Go `pluginpackage`，不新增第二套签名 CLI。stable 模式要求独立读取/发布令牌，先拉取并复验 testing 候选，再核对 SHA、大小、publisher 与 key ID；`-package` 可原样复用 testing 阶段归档的同一 tar.gz，且禁止再次注入构建内容。客户端预检用于尽早诊断，服务端 Approved 强制点仍是最终授权依据。
