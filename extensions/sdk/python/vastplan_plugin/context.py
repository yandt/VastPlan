"""Read-only semantic views over a host-projected CallContext."""

from __future__ import annotations

from dataclasses import dataclass
from types import MappingProxyType
from typing import Mapping, Optional, Tuple

from contract.v1 import contract_pb2


@dataclass(frozen=True)
class ScopeView:
    tenant_id: str
    project_id: Optional[str]


@dataclass(frozen=True)
class CallerView:
    kind: int
    id: str
    scene: str


@dataclass(frozen=True)
class SubjectView:
    id: str
    username: str
    session_id: Optional[str]


@dataclass(frozen=True)
class AuthorizationView:
    is_admin: bool
    system_roles: Tuple[str, ...]
    project_roles: Mapping[str, Tuple[str, ...]]


@dataclass(frozen=True)
class TraceView:
    trace_id: str
    span_id: str
    parent_span_id: Optional[str]


@dataclass(frozen=True)
class RequestControlView:
    deadline_unix_ms: Optional[int]
    idempotency_key: Optional[str]
    call_path: Tuple[str, ...]


@dataclass(frozen=True)
class CredentialView:
    name: str
    scope: Optional[str]


@dataclass(frozen=True)
class ContextViews:
    scope: ScopeView
    caller: CallerView
    subject: SubjectView
    authorization: AuthorizationView
    trace: TraceView
    request: RequestControlView
    credentials: Tuple[CredentialView, ...]
    baggage: Mapping[str, str]

    @classmethod
    def from_wire(cls, wire: contract_pb2.CallContext) -> "ContextViews":
        principal = wire.principal
        trace = wire.trace
        project_roles = MappingProxyType({
            project: tuple(roles.roles) for project, roles in principal.project_roles.items()
        })
        return cls(
            scope=ScopeView(wire.tenant_id, wire.project_id if wire.HasField("project_id") else None),
            caller=CallerView(wire.caller.kind, wire.caller.id, wire.scene),
            subject=SubjectView(
                principal.user_id,
                principal.username,
                principal.session_id if principal.HasField("session_id") else None,
            ),
            authorization=AuthorizationView(
                principal.is_admin, tuple(principal.system_roles), project_roles,
            ),
            trace=TraceView(
                trace.trace_id,
                trace.span_id,
                trace.parent_span_id if trace.HasField("parent_span_id") else None,
            ),
            request=RequestControlView(
                wire.deadline_unix_ms if wire.HasField("deadline_unix_ms") else None,
                wire.idempotency_key if wire.HasField("idempotency_key") else None,
                tuple(wire.call_path),
            ),
            credentials=tuple(CredentialView(
                ref.name, ref.scope if ref.HasField("scope") else None,
            ) for ref in wire.credentials),
            baggage=MappingProxyType(dict(wire.metadata)),
        )
