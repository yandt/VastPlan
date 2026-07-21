import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";

const interactionCapability = "platform.interaction-broker";
const allowedOperations = new Set(["list", "get", "present", "respond"]);

export interface InteractionPort {
  call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

export class AddressingInteractionClient implements InteractionPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    if (!allowedOperations.has(operation)) throw new Error(`Interaction operation 不在白名单: ${operation}`);
    return this.invoker.invoke(principal, {
      capability: interactionCapability, routingDomain: "platform",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, operation, payload, signal);
  }
}
