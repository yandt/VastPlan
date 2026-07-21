import type { Principal } from "../identity/identity-provider";
import type { TrustedCapabilityInvoker } from "./capability-invoker";
import { platformOperationAllowed } from "./platform-capability-policy";
import type { PlatformManagementTarget } from "./platform-management-resolver";

export class ManagementAuthorizationError extends Error {
  public constructor() { super("Management Binding 未授权该操作"); this.name = "ManagementAuthorizationError"; }
}

export interface PlatformCapabilityPort {
  authorize(target: PlatformManagementTarget, capability: string, operation: string, write: boolean): void;
  call(principal: Principal, target: PlatformManagementTarget, capability: string, operation: string, write: boolean, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

export class AddressingPlatformManagementClient implements PlatformCapabilityPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker) {}

  public authorize(target: PlatformManagementTarget, capability: string, operation: string, write: boolean): void {
    if (!platformOperationAllowed(target.service, capability, operation, write)) throw new ManagementAuthorizationError();
  }

  public call(principal: Principal, target: PlatformManagementTarget, capability: string, operation: string, write: boolean, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array> {
    this.authorize(target, capability, operation, write);
    return this.invoker.invoke(principal, {
      capability, logicalService: target.service.logicalService, routingDomain: target.service.routingDomain,
    }, operation, payload, signal);
  }
}
