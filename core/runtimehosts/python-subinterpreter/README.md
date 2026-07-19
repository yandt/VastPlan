# Python Subinterpreter Runtime Host

该受信任宿主要求 CPython 3.14+，在主解释器中维持 VastPlan gRPC 协议，在独立子解释器中加载第一方插件业务代码。这样 `grpcio`/Protobuf 不进入插件子解释器，插件的业务依赖仍必须全部满足多解释器安全要求。

`--probe` 会输出机器可读能力；版本不足或缺少公开 `concurrent.interpreters` API 时明确拒绝启动。它不会静默回退到普通 Python 进程。需要回退的插件必须在清单中明确改用 `driver=python`。

桥接 v1 支持静态贡献与调用；HostCall、事件发布、动态贡献和有状态迁移暂未开放，因此 Runtime Host 不会向协议宿主宣告对应 feature。需要这些能力的 Python 插件暂时继续使用独立进程驱动。
