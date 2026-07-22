import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";

const preferenceCapability = "platform.portal-preference";
const allowedOperations = new Set(["get", "put"]);

export interface PortalPreferencePort {
  call(principal: Principal, operation: "get" | "put", payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

/** Narrow BFF adapter. Scope and subject are fixed before this port is called. */
export class AddressingPortalPreferenceClient implements PortalPreferencePort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public async call(principal: Principal, operation: "get" | "put", payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    if (!allowedOperations.has(operation)) throw new Error(`PortalPreference operation 不在白名单: ${operation}`);
    return this.invoker.invoke(principal, {
      capability: preferenceCapability,
      routingDomain: "platform",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, operation, payload, signal);
  }
}
