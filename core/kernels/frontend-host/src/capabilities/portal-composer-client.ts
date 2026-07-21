import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";
export { CapabilityApplicationError } from "./capability-invoker";

const composerCapability = "platform.portal-composer";
const allowedOperations = new Set([
  "governance", "createDraft", "updateDraft", "list", "submit", "approve", "publish", "audit",
  "createProfileDraft", "updateProfileDraft", "transitionProfile", "createBindingDraft", "updateBindingDraft", "transitionBinding",
  "activate", "rollbackActivation", "listActivations", "listTestTargetBindings", "putTestTargetBinding", "listTestReleases", "createTestRelease", "rollbackTestRelease",
]);

export interface PortalComposerPort {
  call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

/** Narrow Portal Host adapter; it never exposes arbitrary capability routing. */
export class AddressingPortalComposerClient implements PortalComposerPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public async call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    if (!allowedOperations.has(operation)) throw new Error(`Portal Composer operation 不在白名单: ${operation}`);
    return this.invoker.invoke(principal, {
      capability: composerCapability, routingDomain: "platform",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, operation, payload, signal);
  }
}
