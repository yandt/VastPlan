"""Identity-free Python client for the trusted state.shared.v1 service."""

from __future__ import annotations

import base64
import binascii
from dataclasses import dataclass
from datetime import datetime
import json
import re
from typing import Any, Mapping, Optional, Tuple

PROTOCOL = "state.shared.v1"
KERNEL_PREFIX = "kernel.state.shared."
MAX_VALUE_BYTES = 1 << 20
MAX_PAGE_SIZE = 200
_NAMESPACE = re.compile(r"^[a-z][a-z0-9._-]{0,119}$")


class SharedStateError(RuntimeError):
    def __init__(self, code: str, message: str, retryable: bool = False):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.retryable = retryable


@dataclass(frozen=True)
class SharedStateEntry:
    key: str
    value: bytes
    revision: int
    updated_at: str


@dataclass(frozen=True)
class SharedStatePage:
    items: Tuple[SharedStateEntry, ...]
    next_page_cursor: Optional[str] = None


class SharedStateClient:
    def __init__(self, plugin: Any, scope: str, namespace: str):
        if plugin is None or not callable(getattr(plugin, "call", None)) or scope not in ("tenant", "service") or not isinstance(namespace, str) or _NAMESPACE.fullmatch(namespace) is None:
            raise ValueError("Shared State client 配置无效")
        self._plugin, self._scope, self._namespace = plugin, scope, namespace

    def get(self, call_context: Any, key: str) -> SharedStateEntry:
        return parse_shared_state_entry(self._call("get", call_context, {"key": _key(key)}))

    def create(self, call_context: Any, key: str, value: bytes) -> SharedStateEntry:
        return parse_shared_state_entry(self._call("create", call_context, {"key": _key(key), "value": _encode(value)}))

    def update(self, call_context: Any, key: str, value: bytes, expected_revision: int) -> SharedStateEntry:
        if not _revision(expected_revision):
            raise ValueError("Shared State expectedRevision 无效")
        return parse_shared_state_entry(self._call("update", call_context, {"key": _key(key), "value": _encode(value), "expectedRevision": expected_revision}))

    def delete(self, call_context: Any, key: str, expected_revision: int) -> None:
        if not _revision(expected_revision):
            raise ValueError("Shared State expectedRevision 无效")
        value = _object(self._call("delete", call_context, {"key": _key(key), "expectedRevision": expected_revision}), "Shared State ack")
        if set(value) != {"protocol"} or value["protocol"] != PROTOCOL:
            raise ValueError("Shared State ack 无效")

    def list(self, call_context: Any, prefix: str = "", limit: int = 100, page_cursor: Optional[str] = None) -> SharedStatePage:
        if (prefix and not _valid_key(prefix)) or isinstance(limit, bool) or not isinstance(limit, int) or not 1 <= limit <= MAX_PAGE_SIZE or \
                (page_cursor is not None and not _valid_key(page_cursor)):
            raise ValueError("Shared State list 请求无效")
        request = {"prefix": prefix, "limit": limit}
        if page_cursor is not None:
            request["pageCursor"] = page_cursor
        value = _object(self._call("list", call_context, request), "Shared State page")
        if not {"protocol", "items"}.issubset(value) or not set(value).issubset({"protocol", "items", "nextPageCursor"}) or value["protocol"] != PROTOCOL or \
                not isinstance(value["items"], list) or len(value["items"]) > MAX_PAGE_SIZE or \
                ("nextPageCursor" in value and not _valid_key(value["nextPageCursor"])):
            raise ValueError("Shared State page 无效")
        return SharedStatePage(tuple(_entry(item) for item in value["items"]), value.get("nextPageCursor"))

    def _call(self, operation: str, call_context: Any, request: Mapping[str, Any]) -> bytes:
        payload = json.dumps({"scope": self._scope, "namespace": self._namespace, **request}, separators=(",", ":")).encode()
        result, response = self._plugin.call({"extension_point": "kernel.service", "capability": KERNEL_PREFIX + operation}, call_context, payload)
        if isinstance(result, Mapping):
            if result.get("status") == "ok":
                return bytes(response)
            error = result.get("error") if isinstance(result.get("error"), Mapping) else {}
            raise SharedStateError(str(error.get("code", "state.unavailable")), str(error.get("message", "Shared State 调用失败")), bool(error.get("retryable", False)))
        if result is not None and getattr(result, "status", None) == result.STATUS_OK:
            return bytes(response)
        error = result.error if result is not None and result.HasField("error") else None
        raise SharedStateError(error.code if error else "state.unavailable", error.message if error else "Shared State 调用失败", bool(error.retryable) if error else True)


def parse_shared_state_entry(payload: Any) -> SharedStateEntry:
    return _entry(_object(payload, "Shared State entry"))


def is_shared_state_conflict(error: BaseException) -> bool:
    return isinstance(error, SharedStateError) and error.code == "state.conflict"


def is_shared_state_not_found(error: BaseException) -> bool:
    return isinstance(error, SharedStateError) and error.code == "state.not_found"


def _entry(value: Mapping[str, Any]) -> SharedStateEntry:
    if set(value) != {"protocol", "key", "value", "revision", "updatedAt"} or value["protocol"] != PROTOCOL or not _valid_key(value["key"]) or \
            not _revision(value["revision"]) or not _time(value["updatedAt"]):
        raise ValueError("Shared State entry 无效")
    return SharedStateEntry(value["key"], _decode(value["value"]), value["revision"], value["updatedAt"])


def _object(value: Any, name: str) -> Mapping[str, Any]:
    try:
        parsed = json.loads(bytes(value).decode()) if isinstance(value, (bytes, bytearray)) else value
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError(f"{name} 不是有效 JSON") from error
    if not isinstance(parsed, Mapping):
        raise ValueError(f"{name} 必须是对象")
    return parsed


def _encode(value: bytes) -> str:
    if not isinstance(value, (bytes, bytearray)) or len(value) > MAX_VALUE_BYTES:
        raise ValueError("Shared State value 无效")
    return base64.urlsafe_b64encode(bytes(value)).rstrip(b"=").decode()


def _decode(value: Any) -> bytes:
    if not isinstance(value, str):
        raise ValueError("Shared State value 无效")
    try:
        decoded = base64.urlsafe_b64decode(value + "=" * (-len(value) % 4))
    except (ValueError, binascii.Error) as error:
        raise ValueError("Shared State value 无效") from error
    if len(decoded) > MAX_VALUE_BYTES or _encode(decoded) != value:
        raise ValueError("Shared State value 无效")
    return decoded


def _key(value: Any) -> str:
    if not _valid_key(value):
        raise ValueError("Shared State key 无效")
    return value


def _valid_key(value: Any) -> bool:
    return isinstance(value, str) and 1 <= len(value) <= 320 and value == value.strip() and not any(character in value for character in "\x00\r\n")


def _revision(value: Any) -> bool:
    return not isinstance(value, bool) and isinstance(value, int) and value >= 1


def _time(value: Any) -> bool:
    if not isinstance(value, str):
        return False
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
        return "T" in value
    except ValueError:
        return False
