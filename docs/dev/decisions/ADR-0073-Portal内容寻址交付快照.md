# ADR-0073：Portal 内容寻址交付快照

- 状态：已接受
- 日期：2026-07-19

## 背景

旧实现会在每次 `portal-runtime` 和每个 JavaScript 模块请求中重新读取、校验并解包完整插件制品。一个只有数 KB 的前端入口可能位于数 MB 的全栈插件包中，导致小文件首字节仍需数百毫秒，六个模块串行装配后首屏达到数秒。

## 决策

采用发布边界物化、运行边界只读的方案 C：

1. Composer 在 revision 变为 Published/Active 前调用受限宿主能力 `kernel.portal.catalog.materialize`。
2. Catalog 在可信边界完成制品获取、验签、Manifest 校验和入口提取，生成绑定完整 `PortalSpec` 摘要的不可变 Runtime 快照。
3. JavaScript 以实际字节 SHA-256 存为内容寻址对象，同时预生成 gzip；URL 采用 `/v1/portal-modules/{revision}/{sha256}.js`。
4. runtime 和模块 HTTP 请求只读取物化快照/对象。快照缺失或与当前活动解析锁不一致时 fail-closed，不在请求路径回退到昂贵的制品校验。
5. 模块端点仍要求有效会话并核对活动 revision；支持 immutable cache、ETag/304、gzip 与 preload。浏览器继续用 Web Crypto 校验实际字节。
6. 浏览器并行启动所有已锁定模块，并对内容寻址 URL 使用强缓存；Portal Kernel 在等待期间立即显示最小启动界面。

持久交付目录是 Edge 的私有派生数据，不替代原始签名制品或信任根。未来可信反向代理/CDN、Brotli 和多文件模块图均接在该交付端口之后，不进入插件仓库信任模型。

## 结果

- HTTP 热路径不再随全栈插件包体积增长，也不会重复验签和解包。
- 发布耗时增加，但发布失败不会激活缺少交付对象的 revision。
- revision、PortalSpec 摘要、包摘要和入口摘要形成可审计的四层绑定。
- 磁盘对象可跨进程重启复用；未引用对象的回收策略后续由运维任务实现。
