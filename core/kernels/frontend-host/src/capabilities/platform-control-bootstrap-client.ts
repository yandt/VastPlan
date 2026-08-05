import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";

const capability = "platform.database";

export interface PlatformControlBootstrapPort {
  readonly logicalService: string;
  call(principal: Principal, operation: "platformControlStatus" | "platformControlTest" | "platformControlConfigure", payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

// This client is selected only by the trusted Portal Host composition root.
// It deliberately has no caller-supplied service, route, or capability input.
export class AddressingPlatformControlBootstrapClient implements PlatformControlBootstrapPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, public readonly logicalService: string) {}

  public call(principal: Principal, operation: "platformControlStatus" | "platformControlTest" | "platformControlConfigure", payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    return this.invoker.invoke(principal, { capability, logicalService: this.logicalService, routingDomain: "platform" }, operation, payload, signal);
  }
}
