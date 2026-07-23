"""Python consumer client for the identity-free configuration.scoped.v1 port."""

from __future__ import annotations

from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal
import hashlib
import json
import math
import re
from types import MappingProxyType
from typing import Any, Mapping, Optional, Tuple

PROTOCOL = "configuration.scoped.v1"
EXTENSION_POINT = "configuration.scoped-resolver"
CAPABILITY = "configuration.scoped"
MAX_WATCH_TIMEOUT_MS = 30_000

_CONFIGURATION_ID = re.compile(r"^cfg_[a-f0-9]{24}$")
_DIGEST = re.compile(r"^[a-f0-9]{64}$")
_RFC3339 = re.compile(r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$")


@dataclass(frozen=True)
class ScopedResolution:
    protocol: str
    configuration_id: str
    scope: str
    revision: int
    digest: str
    schema_digest: str
    artifact_sha256: str
    values: Mapping[str, Any]
    source: str
    observed_at: str


@dataclass(frozen=True)
class RevisionObservation:
    protocol: str
    configuration_id: str
    changed: bool
    revision: int
    digest: str
    observed_at: str


class ScopedConfigurationClient:
    def __init__(self, plugin: Any):
        if plugin is None or not callable(getattr(plugin, "call", None)):
            raise ValueError("Scoped Configuration client 缺少插件宿主")
        self._plugin = plugin

    def resolve(self, call_context: Any) -> ScopedResolution:
        result, payload = self._plugin.call(_target("resolve"), call_context, b"{}")
        _require_ok(result)
        return parse_scoped_resolution(payload)

    def watch_revision(self, call_context: Any, after_revision: int, after_digest: str, timeout_ms: Optional[int] = None) -> RevisionObservation:
        if isinstance(after_revision, bool) or not isinstance(after_revision, int) or after_revision < 0 or not _digest(after_digest) or \
                (timeout_ms is not None and (isinstance(timeout_ms, bool) or not isinstance(timeout_ms, int) or timeout_ms < 1 or timeout_ms > MAX_WATCH_TIMEOUT_MS)):
            raise ValueError("Scoped Configuration watchRevision 请求无效")
        request = {"afterRevision": after_revision, "afterDigest": after_digest}
        if timeout_ms is not None:
            request["timeoutMs"] = timeout_ms
        result, payload = self._plugin.call(
            _target("watchRevision"), call_context, _json_bytes(request),
            timeout=min(((timeout_ms or MAX_WATCH_TIMEOUT_MS) / 1000.0) + 5.0, 35.0),
        )
        _require_ok(result)
        return parse_revision_observation(payload)


def parse_scoped_resolution(payload: bytes) -> ScopedResolution:
    value = _parse_object(payload, "Scoped Configuration resolution")
    required = {"protocol", "configurationId", "scope", "revision", "digest", "schemaDigest", "artifactSha256", "values", "source", "observedAt"}
    if set(value) != required:
        raise ValueError("Scoped Configuration 响应字段无效")
    values = _normalize_object(value["values"])
    if value["protocol"] != PROTOCOL or not _CONFIGURATION_ID.fullmatch(value["configurationId"]) or value["scope"] not in ("tenant", "user") or \
            not _nonnegative_integer(value["revision"]) or not _digest(value["digest"]) or not _digest(value["schemaDigest"]) or not _digest(value["artifactSha256"]) or \
            value["source"] not in ("seed", "active") or (value["revision"] == 0) != (value["source"] == "seed") or \
            not _time(value["observedAt"]) or digest_scoped_values(values) != value["digest"]:
        raise ValueError("Scoped Configuration resolution 无效")
    return ScopedResolution(
        value["protocol"], value["configurationId"], value["scope"], value["revision"], value["digest"],
        value["schemaDigest"], value["artifactSha256"], _freeze(values), value["source"], value["observedAt"],
    )


def parse_revision_observation(payload: bytes) -> RevisionObservation:
    value = _parse_object(payload, "Scoped Configuration revision observation")
    required = {"protocol", "configurationId", "changed", "revision", "digest", "observedAt"}
    if set(value) != required or value["protocol"] != PROTOCOL or not _CONFIGURATION_ID.fullmatch(value["configurationId"]) or \
            not isinstance(value["changed"], bool) or not _nonnegative_integer(value["revision"]) or not _digest(value["digest"]) or not _time(value["observedAt"]):
        raise ValueError("Scoped Configuration revision observation 无效")
    return RevisionObservation(value["protocol"], value["configurationId"], value["changed"], value["revision"], value["digest"], value["observedAt"])


def digest_scoped_values(values: Mapping[str, Any]) -> str:
    canonical = canonical_json(_normalize_object(values)).encode("utf-8")
    if not canonical or len(canonical) > 64 << 10:
        raise ValueError("Scoped Configuration values 大小无效")
    return hashlib.sha256(canonical).hexdigest()


def canonical_json(value: Any) -> str:
    return _encode_canonical(_normalize_json(value))


def _target(operation: str) -> Mapping[str, str]:
    return {"extension_point": EXTENSION_POINT, "capability": CAPABILITY, "operation": operation}


def _require_ok(result: Any) -> None:
    if isinstance(result, Mapping):
        if set(result) - {"status", "metadata", "error"}:
            raise RuntimeError("Scoped Configuration resolver 返回未知结果字段")
        if result.get("status") == "ok":
            return
        error = result.get("error")
        message = error.get("message") if isinstance(error, Mapping) else "Scoped Configuration resolver 拒绝请求"
        raise RuntimeError(message)
    if result is not None and getattr(result, "status", None) == result.STATUS_OK:
        return
    message = result.error.message if result is not None and result.HasField("error") else "Scoped Configuration resolver 拒绝请求"
    raise RuntimeError(message)


def _parse_object(payload: bytes, name: str) -> Mapping[str, Any]:
    if not isinstance(payload, (bytes, bytearray)) or len(payload) > 4 << 20:
        raise ValueError(f"{name} 大小无效")
    try:
        value = json.loads(bytes(payload).decode("utf-8"))
    except (UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError(f"{name} 不是有效 JSON") from error
    if not isinstance(value, dict):
        raise ValueError(f"{name} 必须是对象")
    return value


def _normalize_object(value: Any) -> Mapping[str, Any]:
    if not isinstance(value, Mapping):
        raise ValueError("Scoped Configuration values 必须是对象")
    return _normalize_json(value)


def _normalize_json(value: Any) -> Any:
    if isinstance(value, Mapping):
        if not all(isinstance(key, str) for key in value):
            raise ValueError("Scoped Configuration key 必须是字符串")
        return {key: _normalize_json(value[key]) for key in sorted(value, key=lambda item: item.encode("utf-8"))}
    if isinstance(value, (list, tuple)):
        return [_normalize_json(item) for item in value]
    if value is None or isinstance(value, (str, bool)):
        return value
    if isinstance(value, int) and not isinstance(value, bool):
        if abs(value) <= 9_007_199_254_740_991:
            return value
        converted = float(value)
        if math.isfinite(converted):
            return converted
        raise ValueError("Scoped Configuration integer 超出 IEEE-754 范围")
    if isinstance(value, float) and math.isfinite(value):
        return value
    raise ValueError("Scoped Configuration values 包含非 JSON 值")


def _freeze(value: Any) -> Any:
    if isinstance(value, dict):
        return MappingProxyType({key: _freeze(child) for key, child in value.items()})
    if isinstance(value, list):
        return tuple(_freeze(child) for child in value)
    return value


def _encode_canonical(value: Any) -> str:
    if isinstance(value, dict):
        return "{" + ",".join(_encode_string(key) + ":" + _encode_canonical(child) for key, child in value.items()) + "}"
    if isinstance(value, list):
        return "[" + ",".join(_encode_canonical(child) for child in value) + "]"
    if isinstance(value, str):
        return _encode_string(value)
    if value is None:
        return "null"
    if value is True:
        return "true"
    if value is False:
        return "false"
    if isinstance(value, int):
        return str(value)
    if isinstance(value, float):
        return _ecmascript_number(value)
    raise ValueError("Scoped Configuration values 包含非 JSON 值")


def _encode_string(value: str) -> str:
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    return encoded.replace("<", "\\u003c").replace(">", "\\u003e").replace("&", "\\u0026").replace("\u2028", "\\u2028").replace("\u2029", "\\u2029")


def _ecmascript_number(value: float) -> str:
    if not math.isfinite(value):
        raise ValueError("Scoped Configuration number 无效")
    if value == 0:
        return "0"
    absolute = abs(value)
    shortest = repr(value).lower()
    if 1e-6 <= absolute < 1e21:
        decimal = format(Decimal(shortest), "f") if "e" in shortest else shortest
        if "." in decimal:
            decimal = decimal.rstrip("0").rstrip(".")
        return decimal
    if "e" not in shortest:
        shortest = format(value, ".15e")
    mantissa, exponent = shortest.split("e")
    mantissa = mantissa.rstrip("0").rstrip(".")
    exponent_value = int(exponent)
    return mantissa + "e" + ("+" if exponent_value >= 0 else "") + str(exponent_value)


def _json_bytes(value: Mapping[str, Any]) -> bytes:
    return json.dumps(value, separators=(",", ":")).encode("utf-8")


def _digest(value: Any) -> bool:
    return isinstance(value, str) and _DIGEST.fullmatch(value) is not None


def _nonnegative_integer(value: Any) -> bool:
    return not isinstance(value, bool) and isinstance(value, int) and value >= 0


def _time(value: Any) -> bool:
    if not isinstance(value, str) or _RFC3339.fullmatch(value) is None:
        return False
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
        return True
    except ValueError:
        return False
