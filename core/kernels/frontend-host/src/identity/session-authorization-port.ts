import type { TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import type { Principal } from "./identity-provider";
import type { AuthenticationAssertion } from "./signed-authentication-assertion";

const capability = "foundation.security.authorization-session";

export interface SessionAuthorization {
  readonly subjectId: string;
  readonly tenantId: string;
  readonly roles: readonly string[];
  readonly policy: { readonly id: string; readonly revision: number; readonly digest: string };
  readonly expiresAt: string;
}

export interface SessionAuthorizationPort {
  resolve(assertion: AuthenticationAssertion, signal?: AbortSignal): Promise<SessionAuthorization>;
}

export class AddressingSessionAuthorization implements SessionAuthorizationPort {
  public constructor(private readonly invoker: TrustedCapabilityInvoker, private readonly logicalService?: string) {}

  public async resolve(assertion: AuthenticationAssertion, signal?: AbortSignal): Promise<SessionAuthorization> {
    const caller: Principal = Object.freeze({ id: "portal-host", tenantId: assertion.tenantId, roles: Object.freeze(["foundation.security.authorization-session.resolve"]), system: true });
    const request = {
      providerProfileId: assertion.providerProfileId,
      issuer: assertion.subject.issuer,
      subject: assertion.subject.id,
      tenantId: assertion.tenantId,
      portalId: assertion.portalId,
      amr: assertion.amr,
      acr: assertion.acr,
    };
    const raw = await this.invoker.invoke(caller, {
      capability, routingDomain: "security",
      ...(this.logicalService === undefined ? {} : { logicalService: this.logicalService }),
    }, "resolve", Buffer.from(JSON.stringify(request)), signal);
    return parseSessionAuthorization(JSON.parse(Buffer.from(raw).toString("utf8")) as unknown, assertion.tenantId);
  }
}

export function parseSessionAuthorization(value: unknown, expectedTenant: string): SessionAuthorization {
  if (!isRecord(value) || !hasOnly(value, ["subjectId", "tenantId", "roles", "policy", "expiresAt"])
    || !safeID(value.subjectId) || value.tenantId !== expectedTenant || !Array.isArray(value.roles) || value.roles.length > 512
    || value.roles.some((role) => !permission(role)) || new Set(value.roles).size !== value.roles.length
    || !isRecord(value.policy) || !hasOnly(value.policy, ["id", "revision", "digest"]) || !safeID(value.policy.id)
    || !Number.isSafeInteger(value.policy.revision) || (value.policy.revision as number) < 1 || typeof value.policy.digest !== "string" || !/^[a-f0-9]{64}$/.test(value.policy.digest)
    || typeof value.expiresAt !== "string" || !Number.isFinite(Date.parse(value.expiresAt))) throw new Error("Authorization Session 响应无效");
  return Object.freeze({
    subjectId: value.subjectId, tenantId: value.tenantId,
    roles: Object.freeze([...(value.roles as string[])]),
    policy: Object.freeze({ id: value.policy.id, revision: value.policy.revision as number, digest: value.policy.digest }),
    expiresAt: value.expiresAt,
  });
}

function permission(value: unknown): value is string { return typeof value === "string" && /^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*){2,15}$/.test(value) && value.length <= 256; }
function safeID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function hasOnly(value: Record<string, unknown>, keys: readonly string[]): boolean { return Object.keys(value).length === keys.length && keys.every((key) => Object.hasOwn(value, key)); }
