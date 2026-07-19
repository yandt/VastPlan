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


MINIMUM_VERSION = (3, 14)
BOOTSTRAP = r"""
import runpy
import sys
import traceback
from pathlib import Path
from vastplan_subinterpreter.plugin import _configure

try:
    _configure(__vastplan_requests, __vastplan_responses)
    sys.argv = [__vastplan_entry, *__vastplan_args]
    sys.path.insert(0, str(Path(__vastplan_entry).resolve().parent))
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
    parser.add_argument("--entry", help="已验签插件入口")
    namespace, plugin_args = parser.parse_known_args(argv)
    if plugin_args and plugin_args[0] == "--":
        plugin_args = plugin_args[1:]
    namespace.plugin_args = tuple(plugin_args)
    if not namespace.probe and not namespace.entry:
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
            "delegation_token": invocation.delegation_token,
            "context": MessageToDict(call_context, preserving_proto_field_name=True),
            "payload": bytes(payload),
        })
        try:
            while True:
                invocation.raise_if_cancelled()
                try:
                    result = response_queue.get(timeout=0.05)
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


def _declaration(responses, timeout: float = 15.0) -> Mapping[str, Any]:
    message = responses.get(timeout=timeout)
    if message.get("type") == "fatal":
        raise RuntimeError(message.get("error", "子解释器启动失败"))
    if message.get("type") != "declare":
        raise RuntimeError("子解释器首条消息必须是 declare")
    return message


def run(entry: str, plugin_args: Sequence[str]) -> int:
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
        plugin.serve()
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


def main(argv: Optional[Sequence[str]] = None) -> int:
    arguments = parse_arguments(tuple(sys.argv[1:] if argv is None else argv))
    if arguments.probe:
        print(json.dumps(runtime_capability(), ensure_ascii=False))
        return 0
    try:
        return run(arguments.entry, arguments.plugin_args)
    except Exception as error:
        print(f"Python Subinterpreter Runtime Host 启动失败: {error}", file=sys.stderr)
        return 78


if __name__ == "__main__":
    raise SystemExit(main())
