#!/usr/bin/env python3
"""Trusted CPython 3.14+ Runtime Host for first-party VastPlan plugins."""

from __future__ import annotations

import argparse
import json
import os
import queue
import signal
import sys
import threading
import time
from pathlib import Path
from typing import Any, Mapping, Optional, Sequence


# Source-tree development fallback. Production Runtime Host artifacts install
# the SDK into their own environment; this path is added only when the monorepo
# layout is actually present.
SOURCE_SDK = Path(__file__).resolve().parents[3] / "extensions" / "sdk" / "python"
if SOURCE_SDK.is_dir() and str(SOURCE_SDK) not in sys.path:
    sys.path.insert(0, str(SOURCE_SDK))


MINIMUM_VERSION = (3, 14)
BOOTSTRAP = r"""
import runpy
import sys
import traceback
from pathlib import Path

try:
    for __vastplan_path_entry in reversed(__vastplan_sys_path):
        if __vastplan_path_entry not in sys.path:
            sys.path.insert(0, __vastplan_path_entry)
    from vastplan_subinterpreter.plugin import _configure
    _configure(__vastplan_requests, __vastplan_responses)
    sys.argv = [__vastplan_entry, *__vastplan_args]
    sys.path.insert(0, str(Path(__vastplan_entry).resolve().parent))
    # Pool mode reserves process stdout for the JSON control channel.
    sys.stdout = sys.stderr
    runpy.run_path(__vastplan_entry, run_name="__main__")
except BaseException:
    __vastplan_responses.put({"type": "fatal", "error": traceback.format_exc()})
"""


def runtime_capability() -> Mapping[str, Any]:
    supported = sys.implementation.name == "cpython" and sys.version_info >= MINIMUM_VERSION
    reason = ""
    if sys.implementation.name != "cpython":
        reason = "只支持 CPython"
    elif sys.version_info < MINIMUM_VERSION:
        reason = f"需要 CPython {MINIMUM_VERSION[0]}.{MINIMUM_VERSION[1]}+"
    else:
        try:
            from concurrent import interpreters  # noqa: F401
        except ImportError as error:
            supported = False
            reason = f"concurrent.interpreters 不可用: {error}"
    return {
        "runtime": "python-subinterpreter",
        "supported": supported,
        "python": ".".join(str(part) for part in sys.version_info[:3]),
        "reason": reason,
    }


def parse_arguments(argv: Sequence[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--probe", action="store_true", help="输出运行时能力 JSON 后退出")
    parser.add_argument("--pool", action="store_true", help="通过 stdin/stdout JSON 行协议托管多个插件")
    parser.add_argument("--entry", help="已验签插件入口")
    namespace, plugin_args = parser.parse_known_args(argv)
    if plugin_args and plugin_args[0] == "--":
        plugin_args = plugin_args[1:]
    namespace.plugin_args = tuple(plugin_args)
    if not namespace.probe and not namespace.pool and not namespace.entry:
        parser.error("缺少必需参数 --entry")
    return namespace


class SubinterpreterBridge:
    def __init__(self, requests, responses):
        self.requests = requests
        self.responses = responses
        self.pending: dict[str, queue.Queue] = {}
        self.lock = threading.Lock()
        self.closed = threading.Event()
        self.receiver = threading.Thread(target=self._receive, name="vastplan-python-subinterp-results", daemon=True)

    def start(self) -> None:
        self.receiver.start()

    def invoke(self, invocation, _host, call_context, payload, target: Mapping[str, str]):
        from contract.v1 import contract_pb2
        from google.protobuf.json_format import MessageToDict

        request_id = os.urandom(12).hex()
        response_queue: queue.Queue = queue.Queue(maxsize=1)
        with self.lock:
            self.pending[request_id] = response_queue
        self.requests.put({
            "type": "invoke",
            "request_id": request_id,
            "extension_point": target["extension_point"],
            "capability": target["capability"],
            "operation": target["operation"],
            "deadline_unix_ms": invocation.deadline_unix_ms,
            "context": MessageToDict(call_context, preserving_proto_field_name=True),
            "payload": bytes(payload),
        })
        try:
            while True:
                invocation.raise_if_cancelled()
                try:
                    result = response_queue.get(timeout=0.05)
                    if result.get("type") == "host_call":
                        self._host_call(invocation, _host, result)
                        continue
                    break
                except queue.Empty:
                    if self.closed.is_set():
                        raise RuntimeError("Python 子解释器已停止")
            if result.get("status") == "ok":
                return contract_pb2.CallResult(
                    status=contract_pb2.CallResult.STATUS_OK,
                    metadata=result.get("metadata", {}),
                ), bytes(result.get("payload", b""))
            error = result.get("error", {})
            return contract_pb2.CallResult(
                status=contract_pb2.CallResult.STATUS_ERROR,
                error=contract_pb2.Error(
                    code=error.get("code", "plugin.handler_error"),
                    message=error.get("message", "子解释器调用失败"),
                    retryable=bool(error.get("retryable", False)),
                ),
            ), bytes(result.get("payload", b""))
        finally:
            with self.lock:
                self.pending.pop(request_id, None)

    def _host_call(self, invocation, host, message: Mapping[str, Any]) -> None:
        from contract.v1 import contract_pb2
        from google.protobuf.json_format import ParseDict

        host_call_id = message.get("host_call_id")
        response: dict[str, Any] = {"type": "host_call_result", "host_call_id": host_call_id}
        try:
            if not isinstance(host_call_id, str) or not host_call_id:
                raise ValueError("子解释器 HostCall 缺少 ID")
            target_value = message.get("target")
            context_value = message.get("context")
            payload = message.get("payload", b"")
            timeout_ms = message.get("timeout_ms")
            if not isinstance(target_value, Mapping) or not isinstance(context_value, Mapping):
                raise ValueError("子解释器 HostCall target/context 无效")
            if not isinstance(payload, (bytes, bytearray)) or len(payload) > 4 << 20:
                raise ValueError("子解释器 HostCall payload 无效")
            if isinstance(timeout_ms, bool) or not isinstance(timeout_ms, int) or not 1 <= timeout_ms <= 30_000:
                raise ValueError("子解释器 HostCall timeout 无效")
            target = ParseDict(dict(target_value), contract_pb2.CallTarget(), ignore_unknown_fields=False)
            context = ParseDict(dict(context_value), contract_pb2.CallContext(), ignore_unknown_fields=False)
            if not target.extension_point or not target.capability:
                raise ValueError("子解释器 HostCall target 不完整")
            invocation.raise_if_cancelled()
            timeout = timeout_ms / 1000.0
            if invocation.deadline_unix_ms is not None:
                remaining = (invocation.deadline_unix_ms - time.time_ns() // 1_000_000) / 1000.0
                if remaining <= 0:
                    raise TimeoutError("VastPlan invocation timed out")
                timeout = min(timeout, remaining)
            result, result_payload = host.call(
                target, context, bytes(payload), timeout=timeout,
                cancelled=lambda: invocation.cancelled,
            )
            response["result"] = _call_result_mapping(result)
            response["payload"] = bytes(result_payload)
        except Exception as error:
            response["bridge_error"] = str(error)
        self.requests.put(response)

    def close(self) -> None:
        if self.closed.is_set():
            return
        self.closed.set()
        self.requests.put({"type": "shutdown"})

    def _receive(self) -> None:
        while not self.closed.is_set():
            message = self.responses.get()
            if message.get("type") == "fatal":
                self.closed.set()
                return
            request_id = message.get("request_id")
            with self.lock:
                target = self.pending.get(request_id)
            if target is not None:
                target.put(message)


def _call_result_mapping(result) -> Mapping[str, Any]:
    from contract.v1 import contract_pb2

    if result.status == contract_pb2.CallResult.STATUS_OK:
        return {"status": "ok", "metadata": dict(result.metadata)}
    error = result.error if result.HasField("error") else None
    return {
        "status": "error",
        "metadata": dict(result.metadata),
        "error": {
            "code": error.code if error is not None else "host_call.error",
            "message": error.message if error is not None else "HostCall failed",
            "retryable": bool(error.retryable) if error is not None else False,
        },
    }


def _declaration(responses, timeout: float = 15.0) -> Mapping[str, Any]:
    message = responses.get(timeout=timeout)
    if message.get("type") == "fatal":
        raise RuntimeError(message.get("error", "子解释器启动失败"))
    if message.get("type") != "declare":
        raise RuntimeError("子解释器首条消息必须是 declare")
    return message


def run(entry: str, plugin_args: Sequence[str], environment: Optional[Mapping[str, str]] = None,
        stop_requested: Optional[threading.Event] = None) -> int:
    capability = runtime_capability()
    if not capability["supported"]:
        raise RuntimeError(capability["reason"])
    entry_path = Path(entry).resolve(strict=True)
    if not entry_path.is_file():
        raise RuntimeError(f"插件入口不是普通文件: {entry_path}")

    from concurrent import interpreters
    from vastplan_plugin import Contribution, Plugin

    requests = interpreters.create_queue()
    responses = interpreters.create_queue()
    interpreter = interpreters.create()
    interpreter.prepare_main(
        __vastplan_entry=str(entry_path),
        __vastplan_args=tuple(plugin_args),
        __vastplan_requests=requests,
        __vastplan_responses=responses,
        __vastplan_sys_path=tuple(sys.path),
    )
    worker = interpreter.call_in_thread(exec, BOOTSTRAP)
    bridge = None
    try:
        declaration = _declaration(responses)
        plugin = Plugin(
            declaration["plugin_id"], declaration["version"], declaration["engines"], features=(),
        )
        bridge = SubinterpreterBridge(requests, responses)
        for item in declaration["contributions"]:
            handlers = {}
            for operation in item["operations"]:
                target = {
                    "extension_point": item["extension_point"],
                    "capability": item["id"],
                    "operation": operation,
                }
                handlers[operation] = lambda *args, _target=target: bridge.invoke(*args, target=_target)
            plugin.contribute(Contribution(
                extension_point=item["extension_point"],
                id=item["id"],
                priority=item["priority"],
                descriptor=item["descriptor"],
                handlers=handlers,
            ))
        bridge.start()
        if stop_requested is not None:
            threading.Thread(
                target=lambda: (stop_requested.wait(), plugin.shutdown()),
                name="vastplan-python-unit-stop", daemon=True,
            ).start()
        plugin.serve(environment=environment)
        return 0
    finally:
        if bridge is not None:
            bridge.close()
        else:
            requests.put({"type": "shutdown"})
        worker.join(timeout=5.0)
        if worker.is_alive():
            os._exit(143)
        interpreter.close()


class RuntimeUnit:
    def __init__(self, unit_id: str, entry: str, plugin_args: Sequence[str],
                 environment: Mapping[str, str], on_exit):
        self.unit_id = unit_id
        self.entry = entry
        self.plugin_args = tuple(plugin_args)
        self.environment = dict(environment)
        self.on_exit = on_exit
        self.stop_requested = threading.Event()
        self.thread = threading.Thread(target=self._run, name=f"vastplan-python-unit-{unit_id}", daemon=True)

    def start(self) -> None:
        self.thread.start()

    def stop(self) -> None:
        # The protocol host sends lifecycle shutdown before this control call.
        # This flag also prevents a slow-starting unit from entering serve.
        self.stop_requested.set()

    def _run(self) -> None:
        error = ""
        try:
            if self.stop_requested.is_set():
                return
            run(self.entry, self.plugin_args, self.environment, self.stop_requested)
        except BaseException as exc:  # the control plane must observe unit death
            error = f"{type(exc).__name__}: {exc}"
            print(f"Python 执行单元失败 unit={self.unit_id}: {error}", file=sys.stderr)
        finally:
            self.on_exit(self, error)


def _write_control(message: Mapping[str, Any], lock: threading.Lock) -> None:
    with lock:
        print(json.dumps(dict(message), ensure_ascii=False), flush=True)


def run_pool() -> int:
    capability = runtime_capability()
    if not capability["supported"]:
        raise RuntimeError(capability["reason"])
    units: dict[str, RuntimeUnit] = {}
    units_lock = threading.Lock()
    output_lock = threading.Lock()
    stopping = threading.Event()

    def unit_exited(unit: RuntimeUnit, error: str) -> None:
        with units_lock:
            units.pop(unit.unit_id, None)
        message: dict[str, Any] = {
            "event": "unit-exited", "unitId": unit.unit_id, "status": "ok",
        }
        if error:
            message["error"] = error
        _write_control(message, output_lock)

    def request_shutdown(*_args) -> None:
        stopping.set()
        with units_lock:
            current = tuple(units.values())
        for unit in current:
            unit.stop()

    signal.signal(signal.SIGINT, request_shutdown)
    signal.signal(signal.SIGTERM, request_shutdown)

    for raw_line in sys.stdin:
        if stopping.is_set():
            break
        if not raw_line.strip():
            continue
        request: Mapping[str, Any] = {}
        try:
            request = json.loads(raw_line)
            request_id = str(request.get("requestId", ""))
            operation = str(request.get("operation", ""))
            if not request_id or not operation:
                raise ValueError("控制请求缺少 requestId/operation")
            if operation == "start":
                unit_id = str(request.get("unitId", ""))
                entry = str(request.get("entry", ""))
                if not unit_id or not entry:
                    raise ValueError("start 缺少 unitId/entry")
                with units_lock:
                    if unit_id in units:
                        raise ValueError(f"执行单元重复: {unit_id}")
                    unit = RuntimeUnit(
                        unit_id, entry, tuple(str(item) for item in request.get("args", ())),
                        {str(key): str(value) for key, value in request.get("environment", {}).items()},
                        unit_exited,
                    )
                    units[unit_id] = unit
                unit.start()
                _write_control({"requestId": request_id, "unitId": unit_id, "status": "ok"}, output_lock)
            elif operation == "stop":
                unit_id = str(request.get("unitId", ""))
                with units_lock:
                    unit = units.get(unit_id)
                if unit is not None:
                    unit.stop()
                _write_control({"requestId": request_id, "unitId": unit_id, "status": "ok"}, output_lock)
            elif operation == "shutdown":
                _write_control({"requestId": request_id, "status": "ok"}, output_lock)
                request_shutdown()
                break
            else:
                raise ValueError(f"未知控制操作: {operation}")
        except Exception as error:
            _write_control({
                "requestId": str(request.get("requestId", "")),
                "status": "error", "error": str(error),
            }, output_lock)

    request_shutdown()
    deadline = time.monotonic() + 5.0
    while time.monotonic() < deadline:
        with units_lock:
            current = tuple(units.values())
        if not current:
            return 0
        for unit in current:
            unit.thread.join(timeout=0.05)
    # A wedged extension/subinterpreter must not keep a retired Runtime Host
    # generation alive indefinitely.
    os._exit(143)


def main(argv: Optional[Sequence[str]] = None) -> int:
    arguments = parse_arguments(tuple(sys.argv[1:] if argv is None else argv))
    if arguments.probe:
        print(json.dumps(runtime_capability(), ensure_ascii=False))
        return 0
    if arguments.pool:
        try:
            return run_pool()
        except Exception as error:
            print(f"Python Subinterpreter Runtime Pool 启动失败: {error}", file=sys.stderr)
            return 78
    try:
        return run(arguments.entry, arguments.plugin_args)
    except Exception as error:
        print(f"Python Subinterpreter Runtime Host 启动失败: {error}", file=sys.stderr)
        return 78


if __name__ == "__main__":
    raise SystemExit(main())
