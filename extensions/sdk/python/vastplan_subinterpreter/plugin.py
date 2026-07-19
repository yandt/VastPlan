from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Any, Callable, Mapping, Optional


Handler = Callable[["InvocationContext", "Plugin", Mapping[str, Any], bytes], Mapping[str, Any]]

_requests = None
_responses = None


def _configure(requests, responses) -> None:
    """Runtime Host only: inject cross-interpreter queues before loading a plugin."""
    global _requests, _responses
    if _requests is not None or _responses is not None:
        raise RuntimeError("子解释器桥接队列不能重复配置")
    _requests = requests
    _responses = responses


@dataclass(frozen=True)
class Contribution:
    extension_point: str
    id: str
    descriptor: bytes
    priority: int = 0
    handlers: Mapping[str, Handler] = field(default_factory=dict)

    def wire(self) -> Mapping[str, Any]:
        return {
            "extension_point": self.extension_point,
            "id": self.id,
            "priority": self.priority,
            "descriptor": bytes(self.descriptor),
            "operations": tuple(self.handlers),
        }


class InvocationContext:
    def __init__(self, deadline_unix_ms: Optional[int], delegation_token: str = ""):
        self.deadline_unix_ms = deadline_unix_ms
        self.delegation_token = delegation_token

    @property
    def cancelled(self) -> bool:
        return self.deadline_unix_ms is not None and time.time_ns() // 1_000_000 >= self.deadline_unix_ms

    def raise_if_cancelled(self) -> None:
        if self.cancelled:
            raise TimeoutError("VastPlan invocation timed out")


class Plugin:
    def __init__(self, plugin_id: str, version: str, engines: Mapping[str, str]):
        self.id = plugin_id
        self.version = version
        self.engines = dict(engines)
        self._contributions: list[Contribution] = []
        self._routes: dict[tuple[str, str, str], Handler] = {}

    def contribute(self, contribution: Contribution) -> None:
        self._contributions.append(contribution)
        for operation, handler in contribution.handlers.items():
            self._routes[(contribution.extension_point, contribution.id, operation)] = handler

    def serve(self) -> None:
        if _requests is None or _responses is None:
            raise RuntimeError("插件必须由 VastPlan Python Subinterpreter Runtime Host 拉起")
        _responses.put({
            "type": "declare",
            "plugin_id": self.id,
            "version": self.version,
            "engines": self.engines,
            "contributions": tuple(item.wire() for item in self._contributions),
        })
        while True:
            message = _requests.get()
            kind = message.get("type")
            if kind == "shutdown":
                return
            if kind != "invoke":
                continue
            self._invoke(message)

    def _invoke(self, message: Mapping[str, Any]) -> None:
        request_id = message["request_id"]
        key = (message["extension_point"], message["capability"], message.get("operation", ""))
        handler = self._routes.get(key) or self._routes.get((key[0], key[1], ""))
        if handler is None:
            _responses.put(call_error("capability.not_found", "插件未实现目标能力", request_id=request_id))
            return
        invocation = InvocationContext(message.get("deadline_unix_ms"), message.get("delegation_token", ""))
        try:
            result = dict(handler(invocation, self, message.get("context", {}), bytes(message.get("payload", b""))))
            result["type"] = "result"
            result["request_id"] = request_id
        except Exception as error:
            result = call_error("plugin.handler_error", str(error), request_id=request_id)
        _responses.put(result)

    def call(self, *_args, **_kwargs):
        raise RuntimeError("子解释器桥接 v1 尚未开放 HostCall；请改用独立 Python 进程驱动")

    def publish_event(self, *_args, **_kwargs):
        raise RuntimeError("子解释器桥接 v1 尚未开放事件发布；请改用独立 Python 进程驱动")


def call_ok(payload: bytes = b"", metadata: Optional[Mapping[str, str]] = None) -> Mapping[str, Any]:
    return {
        "type": "result",
        "status": "ok",
        "payload": bytes(payload),
        "metadata": dict(metadata or {}),
    }


def call_error(code: str, message: str, *, request_id: str = "", retryable: bool = False) -> Mapping[str, Any]:
    return {
        "type": "result",
        "request_id": request_id,
        "status": "error",
        "error": {"code": code, "message": message, "retryable": retryable},
        "payload": b"",
    }
