"""Closed, non-sensitive ManagedCredentialRef model shared by Python plugins."""

from __future__ import annotations

from dataclasses import dataclass
import re
from types import MappingProxyType
from typing import Any, Mapping, Optional, Sequence

_HANDLE = re.compile(r"^credential://managed/[A-Za-z0-9._~-]+$")
_OWNER = re.compile(r"^[a-z0-9]+(?:[.-][a-z0-9]+)+$")
_PURPOSE = re.compile(r"^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$")
_FIELD = re.compile(r"^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)*$")


@dataclass(frozen=True)
class ManagedCredentialRef:
    handle: str
    scope: str
    owner: str
    purpose: str
    version: int
    name: Optional[str] = None

    @classmethod
    def from_mapping(cls, value: Mapping[str, Any], allowed_scopes: Sequence[str] = ("tenant", "service")) -> "ManagedCredentialRef":
        if not isinstance(value, Mapping):
            raise ValueError("Managed CredentialRef 必须是对象")
        required = {"handle", "scope", "owner", "purpose", "version"}
        if not required.issubset(value) or not set(value).issubset(required | {"name"}):
            raise ValueError("Managed CredentialRef 字段无效")
        ref = cls(value["handle"], value["scope"], value["owner"], value["purpose"], value["version"], value.get("name"))
        if not isinstance(ref.handle, str) or len(ref.handle) > 256 or not _HANDLE.fullmatch(ref.handle) or \
                ref.scope not in allowed_scopes or not isinstance(ref.owner, str) or len(ref.owner) > 160 or not _OWNER.fullmatch(ref.owner) or \
                not isinstance(ref.purpose, str) or len(ref.purpose) > 160 or not _PURPOSE.fullmatch(ref.purpose) or \
                isinstance(ref.version, bool) or not isinstance(ref.version, int) or ref.version < 1 or \
                (ref.name is not None and (not isinstance(ref.name, str) or len(ref.name) < 1 or len(ref.name) > 160)):
            raise ValueError("Managed CredentialRef 无效")
        return ref

    def as_dict(self) -> Mapping[str, Any]:
        value = {"handle": self.handle, "scope": self.scope, "owner": self.owner, "purpose": self.purpose, "version": self.version}
        if self.name is not None:
            value["name"] = self.name
        return MappingProxyType(value)


def managed_credential_refs(value: Optional[Mapping[str, Mapping[str, Any]]], allowed_scopes: Sequence[str] = ("tenant",), maximum: int = 64) -> Mapping[str, ManagedCredentialRef]:
    if value is None:
        return MappingProxyType({})
    if not isinstance(value, Mapping) or isinstance(maximum, bool) or not isinstance(maximum, int) or maximum < 1 or len(value) > maximum:
        raise ValueError("managedCredentials 数量无效")
    normalized = {}
    for field in sorted(value, key=lambda item: item.encode("utf-8")):
        if not isinstance(field, str) or len(field) > 80 or not _FIELD.fullmatch(field):
            raise ValueError("managedCredentials 字段无效")
        normalized[field] = ManagedCredentialRef.from_mapping(value[field], allowed_scopes)
    return MappingProxyType(normalized)
