import { randomBytes } from "node:crypto";
import type { NodeAddressingClient } from "@vastplan/addressing-node";
import type { Principal } from "../identity/identity-provider";

export interface CapabilityRoute {
  capability: string;
  routingDomain: string;
  logicalService?: string;
}

export interface TrustedCapabilityInvoker {
  invoke(principal: Principal, route: CapabilityRoute, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

export class CapabilityApplicationError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "CapabilityApplicationError";
  }
}

export class AddressingCapabilityInvoker implements TrustedCapabilityInvoker {
  public constructor(private readonly addressing: Pick<NodeAddressingClient, "invoke">) {}

  public async invoke(principal: Principal, route: CapabilityRoute, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    const response = await this.addressing.invoke({
      extension_point: "tool.package", capability: route.capability, operation, routing_domain: route.routingDomain,
      ...(route.logicalService === undefined ? {} : { logical_service: route.logicalService }),
    }, {
      caller: { kind: principal.system === true ? 4 : 1, id: principal.id },
      principal: { user_id: principal.id, tenant_id: principal.tenantId, system_roles: [...principal.roles], is_admin: principal.roles.includes("platform.admin") },
      scene: "portal.bff", tenant_id: principal.tenantId,
      trace: { trace_id: randomBytes(16).toString("hex"), span_id: randomBytes(8).toString("hex") },
    }, payload, signal);
    if (response.result.status !== 1) throw new CapabilityApplicationError(response.result.error?.code ?? "capability.failed", response.result.error?.message ?? "Capability 调用失败");
    return response.payload;
  }
}
