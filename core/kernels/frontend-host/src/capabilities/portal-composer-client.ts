import { randomBytes } from "node:crypto";
import type { NodeAddressingClient } from "@vastplan/addressing-node";
import type { Principal } from "../identity/identity-provider";

const composerCapability = "platform.portal-composer";
const allowedOperations = new Set([
  "governance", "createDraft", "updateDraft", "list", "submit", "approve", "publish", "audit",
  "createProfileDraft", "updateProfileDraft", "transitionProfile", "createBindingDraft", "updateBindingDraft", "transitionBinding",
  "activate", "rollbackActivation", "listActivations", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease",
]);

export interface PortalComposerPort {
  call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

export class CapabilityApplicationError extends Error {
  public constructor(public readonly code: string, message: string) {
    super(message);
    this.name = "CapabilityApplicationError";
  }
}

/** Narrow Portal Host adapter; it never exposes arbitrary capability routing. */
export class AddressingPortalComposerClient implements PortalComposerPort {
  public constructor(private readonly addressing: NodeAddressingClient, private readonly logicalService?: string) {}

  public async call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    if (!allowedOperations.has(operation)) throw new Error(`Portal Composer operation 不在白名单: ${operation}`);
    const traceID = randomBytes(16).toString("hex");
    const spanID = randomBytes(8).toString("hex");
    const response = await this.addressing.invoke({
      extension_point: "tool.package", capability: composerCapability, operation, routing_domain: "platform",
      ...(this.logicalService === undefined ? {} : { logical_service: this.logicalService }),
    }, {
      caller: { kind: principal.system === true ? 4 : 1, id: principal.id },
      principal: { user_id: principal.id, tenant_id: principal.tenantId, system_roles: [...principal.roles], is_admin: principal.roles.includes("platform.admin") },
      scene: "portal.bff", tenant_id: principal.tenantId, trace: { trace_id: traceID, span_id: spanID },
    }, payload, signal);
    if (response.result.status !== 1) throw new CapabilityApplicationError(response.result.error?.code ?? "capability.failed", response.result.error?.message ?? "Portal Composer 调用失败");
    return response.payload;
  }
}
