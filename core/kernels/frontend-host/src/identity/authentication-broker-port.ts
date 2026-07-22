import type { Principal } from "./identity-provider";
import type { TrustedCapabilityInvoker } from "../capabilities/capability-invoker";

const capability = "foundation.security.authentication.broker";

export interface AuthenticationBrokerPort {
  call(tenantId: string, operation: string, payload: unknown, signal?: AbortSignal): Promise<unknown>;
}

export class AddressingAuthenticationBroker implements AuthenticationBrokerPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public async call(tenantId: string, operation: string, payload: unknown, signal?: AbortSignal): Promise<unknown> {
    if (!allowedOperations.has(operation)) throw new Error(`Authentication Broker operation 不在白名单: ${operation}`);
    const principal: Principal = Object.freeze({
      id: "portal-host", tenantId,
      roles: Object.freeze(["foundation.security.authentication.providers.broker.invoke"]), system: true,
    });
    const raw = await this.invoker.invoke(principal, {
      capability, routingDomain: "security",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, operation, Buffer.from(JSON.stringify(payload)), signal);
    try { return JSON.parse(Buffer.from(raw).toString("utf8")) as unknown; }
    catch { throw new Error("Authentication Broker 返回无效 JSON"); }
  }
}

const allowedOperations = new Set(["describe", "begin", "continue", "resend", "cancel", "health", "consumeAssertion", "beginProviderTest"]);
