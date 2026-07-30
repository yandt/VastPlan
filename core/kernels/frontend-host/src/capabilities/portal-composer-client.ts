import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";
import { portalComposerOperationSet, type PortalComposerOperation } from "./portal-composer-operations.generated";
export { CapabilityApplicationError } from "./capability-invoker";

const composerCapability = "platform.portal-composer";

export interface PortalComposerPort {
  call(principal: Principal, operation: PortalComposerOperation, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

/** Narrow Portal Host adapter; it never exposes arbitrary capability routing. */
export class AddressingPortalComposerClient implements PortalComposerPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public async call(principal: Principal, operation: PortalComposerOperation, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    if (!portalComposerOperationSet.has(operation)) throw new Error(`Portal Composer operation 不在签名 Capability Contract: ${operation}`);
    return this.invoker.invoke(principal, {
      capability: composerCapability, routingDomain: "platform",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, operation, payload, signal);
  }
}
