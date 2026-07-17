from __future__ import annotations

import os
import queue
import threading
import time
import uuid
from dataclasses import dataclass, field
from typing import Callable, Iterable, Mapping, Optional, Tuple

import grpc

from contract.v1 import contract_pb2
from pluginhost.v1 import pluginhost_pb2, pluginhost_pb2_grpc


MAGIC = "VASTPLAN_PLUGIN_V1"
MAGIC_ENV = "VASTPLAN_PLUGIN_MAGIC"
HOST_ENV = "VASTPLAN_HOST_ADDR"
TOKEN_ENV = "VASTPLAN_LAUNCH_TOKEN"
SESSION_METADATA = "vastplan-session-id"
FEATURES = ("channel.cancel.v1", "contribution.dynamic.v1", "event.publish.v1")
MAX_PAYLOAD_BYTES = 4 << 20
MAX_METADATA_BYTES = 16 << 10
MAX_CONCURRENT_CALLS = 256
ERROR_PLUGIN_INACTIVE = "plugin.inactive"
ERROR_CONCURRENCY_LIMITED = "resource.concurrency_limited"
ERROR_PAYLOAD_TOO_LARGE = "resource.payload_too_large"
ERROR_METADATA_TOO_LARGE = "resource.metadata_too_large"
ERROR_CAPABILITY_NOT_FOUND = "capability.not_found"
ERROR_PLUGIN_HANDLER = "plugin.handler_error"

Handler = Callable[["InvocationContext", "Plugin", contract_pb2.CallContext, bytes], Tuple[contract_pb2.CallResult, bytes]]


@dataclass(frozen=True)
class Contribution:
    extension_point: str
    id: str
    descriptor: bytes
    priority: int = 0
    handlers: Mapping[str, Handler] = field(default_factory=dict)

    def wire(self) -> pluginhost_pb2.Contribution:
        return pluginhost_pb2.Contribution(
            extension_point=self.extension_point,
            id=self.id,
            priority=self.priority,
            descriptor_json=self.descriptor,
        )


class InvocationContext:
    def __init__(self, deadline_unix_ms: Optional[int] = None):
        self._cancelled = threading.Event()
        self.deadline_unix_ms = deadline_unix_ms

    @property
    def cancelled(self) -> bool:
        return self._cancelled.is_set() or (
            self.deadline_unix_ms is not None and time.time_ns() // 1_000_000 >= self.deadline_unix_ms
        )

    def raise_if_cancelled(self) -> None:
        if self.cancelled:
            raise TimeoutError("VastPlan invocation cancelled or timed out")


class Plugin:
    def __init__(self, plugin_id: str, version: str, engines: Mapping[str, str]):
        self.id = plugin_id
        self.version = version
        self.engines = dict(engines)
        self._contributions: list[Contribution] = []
        self._routes: dict[tuple[str, str, str], Handler] = {}
        self._contribution_lock = threading.Lock()
        self._outgoing: queue.Queue[Optional[pluginhost_pb2.FromPlugin]] = queue.Queue(maxsize=512)
        self._pending: dict[str, queue.Queue[pluginhost_pb2.FromHost]] = {}
        self._pending_lock = threading.Lock()
        self._calls: dict[str, InvocationContext] = {}
        self._calls_lock = threading.Lock()
        self._slots = threading.BoundedSemaphore(MAX_CONCURRENT_CALLS)
        self._active = False
        self._features: set[str] = set()
        self._stream_started = False
        self._event_handler: Optional[Callable[[contract_pb2.CallEvent], None]] = None
        self._lifecycle_queue: queue.Queue[Optional[pluginhost_pb2.Lifecycle]] = queue.Queue(maxsize=512)

    def contribute(self, contribution: Contribution) -> None:
        if self._stream_started:
            raise RuntimeError("serve 后请使用 register_contribution")
        self._install_local(contribution)

    def on_event(self, handler: Callable[[contract_pb2.CallEvent], None]) -> None:
        self._event_handler = handler

    def serve(self) -> None:
        if os.getenv(MAGIC_ENV) != MAGIC:
            raise RuntimeError("magic cookie 不匹配：插件必须由 VastPlan 宿主拉起")
        address = os.getenv(HOST_ENV)
        if not address:
            raise RuntimeError(f"未注入宿主地址 {HOST_ENV}")
        channel = grpc.insecure_channel(
            address,
            options=(("grpc.max_receive_message_length", 4_456_448), ("grpc.max_send_message_length", 4_456_448)),
        )
        stub = pluginhost_pb2_grpc.PluginHostStub(channel)
        ack = stub.Handshake(pluginhost_pb2.Hello(
            proto_versions=(1,), magic=MAGIC, plugin_id=self.id, plugin_version=self.version,
            engines=self.engines, launch_token=os.getenv(TOKEN_ENV, ""), features=FEATURES,
        ))
        if ack.negotiated_proto != 1:
            raise RuntimeError(f"宿主协商了不支持的协议版本 {ack.negotiated_proto}")
        self._features = set(ack.negotiated_features)
        self._stream_started = True
        lifecycle_worker = threading.Thread(target=self._lifecycle_loop, daemon=True)
        lifecycle_worker.start()
        responses = stub.Channel(self._request_iterator(), metadata=((SESSION_METADATA, ack.session_id),))
        with self._contribution_lock:
            declared = tuple(contribution.wire() for contribution in self._contributions)
        self._send(pluginhost_pb2.FromPlugin(declare=pluginhost_pb2.Declaration(
            contributions=declared
        )))
        try:
            for message in responses:
                self._dispatch(message)
        finally:
            try:
                self._lifecycle_queue.put_nowait(None)
            except queue.Full:
                pass
            try:
                self._outgoing.put_nowait(None)
            except queue.Full:
                pass
            channel.close()

    def call(self, target: contract_pb2.CallTarget, call_context: contract_pb2.CallContext, payload: bytes,
             timeout: float = 30.0) -> tuple[contract_pb2.CallResult, bytes]:
        if len(payload) > MAX_PAYLOAD_BYTES:
            raise ValueError("HostCall payload 超过协议上限")
        if call_context.ByteSize() > MAX_METADATA_BYTES:
            raise ValueError("HostCall context 超过 metadata 上限")
        request_id = self._new_id("hc")
        response_queue = self._start_pending(request_id)
        try:
            self._send(pluginhost_pb2.FromPlugin(host_call=pluginhost_pb2.InvokeRequest(
                request_id=request_id, target=target, context=call_context, payload=payload,
            )))
            try:
                response = response_queue.get(timeout=timeout).host_call_result
            except queue.Empty as error:
                if "channel.cancel.v1" in self._features:
                    self._send(pluginhost_pb2.FromPlugin(cancel=pluginhost_pb2.Cancel(request_id=request_id)))
                raise TimeoutError("HostCall timed out") from error
            if len(response.payload) > MAX_PAYLOAD_BYTES:
                raise RuntimeError("HostCall 响应 payload 超过协议上限")
            return response.result, response.payload
        finally:
            self._finish_pending(request_id)

    def publish_event(self, event: contract_pb2.CallEvent) -> None:
        self._require_feature("event.publish.v1")
        if not event.id or not event.type:
            raise ValueError("event id/type 不能为空")
        if len(event.payload) > MAX_PAYLOAD_BYTES:
            raise ValueError("event payload 超过协议上限")
        self._send(pluginhost_pb2.FromPlugin(event=pluginhost_pb2.EventEnvelope(event=event)))

    def register_contribution(self, contribution: Contribution, timeout: float = 30.0) -> None:
        self._install_local(contribution)
        request_id = self._new_id("cu")
        try:
            response = self._contribution_update(request_id, pluginhost_pb2.FromPlugin(register=
                pluginhost_pb2.RegisterContributions(request_id=request_id, contributions=(contribution.wire(),))), timeout)
        except Exception:
            self._remove_local(contribution.extension_point, contribution.id)
            raise
        if response.rejected:
            self._remove_local(contribution.extension_point, contribution.id)
            raise RuntimeError(f"宿主拒绝动态贡献: {dict(response.rejected)}")

    def unregister_contribution(self, extension_point: str, contribution_id: str, timeout: float = 30.0) -> None:
        request_id = self._new_id("cu")
        response = self._contribution_update(request_id, pluginhost_pb2.FromPlugin(unregister=
            pluginhost_pb2.UnregisterContributions(request_id=request_id, contributions=(
                pluginhost_pb2.ContributionRef(extension_point=extension_point, id=contribution_id),))), timeout)
        if response.rejected:
            raise RuntimeError(f"宿主拒绝动态卸载: {dict(response.rejected)}")
        self._remove_local(extension_point, contribution_id)

    def _contribution_update(self, request_id: str, message: pluginhost_pb2.FromPlugin,
                             timeout: float) -> pluginhost_pb2.ContributionUpdateAck:
        self._require_feature("contribution.dynamic.v1")
        response_queue = self._start_pending(request_id)
        try:
            self._send(message)
            try:
                return response_queue.get(timeout=timeout).contribution_update_ack
            except queue.Empty as error:
                raise TimeoutError("动态贡献更新超时") from error
        finally:
            self._finish_pending(request_id)

    def _request_iterator(self) -> Iterable[pluginhost_pb2.FromPlugin]:
        while True:
            message = self._outgoing.get()
            if message is None:
                return
            yield message

    def _dispatch(self, message: pluginhost_pb2.FromHost) -> None:
        kind = message.WhichOneof("msg")
        if kind == "invoke":
            if not self._active:
                self._reply_error(message.invoke, ERROR_PLUGIN_INACTIVE, "插件未激活")
                return
            if not self._slots.acquire(blocking=False):
                self._reply_error(message.invoke, ERROR_CONCURRENCY_LIMITED, "插件处理器并发达到上限")
                return
            deadline = message.invoke.context.deadline_unix_ms if message.invoke.context.HasField("deadline_unix_ms") else None
            invocation = InvocationContext(deadline)
            with self._calls_lock:
                self._calls[message.invoke.request_id] = invocation
            threading.Thread(target=self._invoke, args=(message.invoke, invocation), daemon=True).start()
        elif kind == "lifecycle":
            try:
                self._lifecycle_queue.put_nowait(message.lifecycle)
            except queue.Full:
                self._send(pluginhost_pb2.FromPlugin(lifecycle_ack=pluginhost_pb2.LifecycleAck(
                    request_id=message.lifecycle.request_id, ready=False, message="生命周期 pending 队列已满")))
        elif kind == "ping":
            self._send(pluginhost_pb2.FromPlugin(pong=pluginhost_pb2.Pong(request_id=message.ping.request_id)))
        elif kind in ("host_call_result", "contribution_update_ack"):
            payload = getattr(message, kind)
            with self._pending_lock:
                waiting = self._pending.get(payload.request_id)
            if waiting is not None:
                try:
                    waiting.put_nowait(message)
                except queue.Full:
                    pass
        elif kind == "cancel" and "channel.cancel.v1" in self._features:
            with self._calls_lock:
                invocation = self._calls.get(message.cancel.request_id)
            if invocation is not None:
                invocation._cancelled.set()
        elif kind == "event" and self._event_handler is not None:
            if self._slots.acquire(blocking=False):
                threading.Thread(target=self._run_event_handler, args=(message.event.event,), daemon=True).start()

    def _invoke(self, request: pluginhost_pb2.InvokeRequest, invocation: InvocationContext) -> None:
        response: pluginhost_pb2.InvokeResponse
        if len(request.payload) > MAX_PAYLOAD_BYTES:
            response = self._error_response(request.request_id, ERROR_PAYLOAD_TOO_LARGE, "payload 超过协议上限")
            self._finish_invoke(request.request_id, response)
            return
        if request.context.ByteSize() > MAX_METADATA_BYTES:
            response = self._error_response(request.request_id, ERROR_METADATA_TOO_LARGE, "CallContext 超过 metadata 上限")
            self._finish_invoke(request.request_id, response)
            return
        operation = request.target.operation if request.target.HasField("operation") else ""
        with self._contribution_lock:
            handler = self._routes.get((request.target.extension_point, request.target.capability, operation))
            handler = handler or self._routes.get((request.target.extension_point, request.target.capability, ""))
        if handler is None:
            response = self._error_response(request.request_id, ERROR_CAPABILITY_NOT_FOUND, "插件未实现目标能力")
            self._finish_invoke(request.request_id, response)
            return
        try:
            result, payload = handler(invocation, self, request.context, request.payload)
            if len(payload) > MAX_PAYLOAD_BYTES:
                response = self._error_response(request.request_id, ERROR_PAYLOAD_TOO_LARGE, "响应 payload 超过协议上限")
            else:
                response = pluginhost_pb2.InvokeResponse(request_id=request.request_id, result=result, payload=payload)
        except Exception as error:  # Handler errors are application-level results.
            response = self._error_response(request.request_id, ERROR_PLUGIN_HANDLER, str(error))
        self._finish_invoke(request.request_id, response)

    def _finish_invoke(self, request_id: str, response: pluginhost_pb2.InvokeResponse) -> None:
        with self._calls_lock:
            self._calls.pop(request_id, None)
        try:
            self._send(pluginhost_pb2.FromPlugin(invoke_result=response))
        finally:
            self._slots.release()

    def _run_event_handler(self, event: contract_pb2.CallEvent) -> None:
        try:
            if self._event_handler is not None:
                self._event_handler(event)
        finally:
            self._slots.release()

    def _lifecycle_loop(self) -> None:
        while True:
            lifecycle = self._lifecycle_queue.get()
            if lifecycle is None:
                return
            self._lifecycle(lifecycle)

    def _lifecycle(self, lifecycle: pluginhost_pb2.Lifecycle) -> None:
        ready, message = True, ""
        if lifecycle.op == pluginhost_pb2.Lifecycle.OP_ACTIVATE:
            self._active = True
        elif lifecycle.op in (pluginhost_pb2.Lifecycle.OP_DEACTIVATE, pluginhost_pb2.Lifecycle.OP_DRAIN,
                              pluginhost_pb2.Lifecycle.OP_SHUTDOWN):
            self._active = False
            while lifecycle.op != pluginhost_pb2.Lifecycle.OP_DEACTIVATE:
                with self._calls_lock:
                    if not self._calls:
                        break
                time.sleep(0.01)
        else:
            ready, message = False, "Python SDK 尚未配置该生命周期处理器"
        ack = pluginhost_pb2.LifecycleAck(request_id=lifecycle.request_id, ready=ready)
        if message:
            ack.message = message
        self._send(pluginhost_pb2.FromPlugin(lifecycle_ack=ack))

    def _install_local(self, contribution: Contribution) -> None:
        with self._contribution_lock:
            self._contributions.append(contribution)
            for operation, handler in contribution.handlers.items():
                self._routes[(contribution.extension_point, contribution.id, operation)] = handler

    def _remove_local(self, extension_point: str, contribution_id: str) -> None:
        with self._contribution_lock:
            self._contributions = [item for item in self._contributions
                                   if (item.extension_point, item.id) != (extension_point, contribution_id)]
            for key in tuple(self._routes):
                if key[:2] == (extension_point, contribution_id):
                    del self._routes[key]

    def _send(self, message: pluginhost_pb2.FromPlugin) -> None:
        self._outgoing.put(message, timeout=30.0)

    def _start_pending(self, request_id: str) -> queue.Queue[pluginhost_pb2.FromHost]:
        response_queue: queue.Queue[pluginhost_pb2.FromHost] = queue.Queue(maxsize=1)
        with self._pending_lock:
            if len(self._pending) >= 512:
                raise RuntimeError("pending 请求达到上限")
            self._pending[request_id] = response_queue
        return response_queue

    def _finish_pending(self, request_id: str) -> None:
        with self._pending_lock:
            self._pending.pop(request_id, None)

    def _reply_error(self, request: pluginhost_pb2.InvokeRequest, code: str, message: str) -> None:
        self._send(pluginhost_pb2.FromPlugin(invoke_result=self._error_response(request.request_id, code, message)))

    @staticmethod
    def _error_response(request_id: str, code: str, message: str) -> pluginhost_pb2.InvokeResponse:
        return pluginhost_pb2.InvokeResponse(request_id=request_id, result=contract_pb2.CallResult(
            status=contract_pb2.CallResult.STATUS_ERROR,
            error=contract_pb2.Error(code=code, message=message, retryable=False),
        ))

    def _require_feature(self, feature: str) -> None:
        if feature not in self._features:
            raise RuntimeError(f"宿主未协商协议能力 {feature}")

    @staticmethod
    def _new_id(prefix: str) -> str:
        return f"{prefix}-{uuid.uuid4().hex}"
