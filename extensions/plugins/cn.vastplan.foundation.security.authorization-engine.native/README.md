# Native Authorization Engine

`cn.vastplan.foundation.security.authorization-engine.native` 是默认 Go `authorization.engine.v1` Provider，提供 `prepare/evaluate/explain/health` 与最长五分钟的 Decision Proof。它只接受可信 system caller，并随 `platform.authorization` 领导者运行；作为独立 foundation 插件，它不会让每内核 Enforcer 因同时暴露 Tool 而失去可重复附着资格。

每内核 PEP 仍由 `authorization-enforcer` 执行并负责快照验签、audience、LKG 与最终缓存上限；替换 Engine Provider 不会移动这些信任边界。
