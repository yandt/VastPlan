from __future__ import annotations

import time
import uuid
from dataclasses import dataclass, field
from typing import Any, Callable, Mapping, Optional


Handler = Callable[["InvocationContext", "Plugin", Mapping[str, Any], bytes], Mapping[str, Any]]
MAX_PAYLOAD_BYTES = 4 << 20
MAX_HOST_CALL_TIMEOUT = 30.0

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
        self._active_request_id = ""
        self._active_invocation: Optional[InvocationContext] = None

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
        # The opaque host delegation token deliberately stays in the trusted
        # main interpreter. Child code receives only the projected context.
        invocation = InvocationContext(message.get("deadline_unix_ms"))
        try:
            self._active_request_id = request_id
            self._active_invocation = invocation
            result = dict(handler(invocation, self, message.get("context", {}), bytes(message.get("payload", b""))))
            result["type"] = "result"
            result["request_id"] = request_id
        except Exception as error:
            result = call_error("plugin.handler_error", str(error), request_id=request_id)
        finally:
            self._active_request_id = ""
            self._active_invocation = None
        _responses.put(result)

    def call(self, target: Mapping[str, Any], call_context: Mapping[str, Any], payload: bytes,
             timeout: float = MAX_HOST_CALL_TIMEOUT):
        if not self._active_request_id or self._active_invocation is None:
            raise RuntimeError("HostCall 只能在插件调用处理器内发起")
        if not isinstance(target, Mapping) or not isinstance(call_context, Mapping):
            raise TypeError("子解释器 HostCall target/context 必须是纯 Python 映射")
        if not isinstance(payload, (bytes, bytearray)) or len(payload) > MAX_PAYLOAD_BYTES:
            raise ValueError("HostCall payload 超过协议上限")
        if isinstance(timeout, bool) or not isinstance(timeout, (int, float)) or timeout <= 0:
            raise ValueError("HostCall timeout 无效")
        self._active_invocation.raise_if_cancelled()
        timeout = min(float(timeout), MAX_HOST_CALL_TIMEOUT)
        if self._active_invocation.deadline_unix_ms is not None:
            remaining = (self._active_invocation.deadline_unix_ms - time.time_ns() // 1_000_000) / 1000.0
            if remaining <= 0:
                raise TimeoutError("VastPlan invocation timed out")
            timeout = min(timeout, remaining)
        host_call_id = "hc-" + uuid.uuid4().hex
        _responses.put({
            "type": "host_call",
            "request_id": self._active_request_id,
            "host_call_id": host_call_id,
            "target": dict(target),
            "context": dict(call_context),
            "payload": bytes(payload),
            "timeout_ms": max(1, int(timeout * 1000)),
        })
        deadline = time.monotonic() + timeout
        while True:
            self._active_invocation.raise_if_cancelled()
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                raise TimeoutError("HostCall timed out")
            try:
                response = _requests.get(timeout=min(remaining, 0.05))
            except Exception as error:
                if error.__class__.__name__ in ("Empty", "QueueEmpty"):
                    continue
                raise
            kind = response.get("type")
            if kind == "shutdown":
                raise RuntimeError("Python 子解释器正在停止")
            if kind != "host_call_result" or response.get("host_call_id") != host_call_id:
                raise RuntimeError("子解释器 HostCall 收到乱序桥接消息")
            error = response.get("bridge_error")
            if error:
                raise RuntimeError(str(error))
            response_payload = bytes(response.get("payload", b""))
            if len(response_payload) > MAX_PAYLOAD_BYTES:
                raise RuntimeError("HostCall 响应 payload 超过协议上限")
            result = response.get("result")
            if not isinstance(result, Mapping):
                raise RuntimeError("HostCall 响应缺少结果")
            return dict(result), response_payload

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
