import type { Principal } from "./identity-provider";
import type { SignedAuthenticationAssertion } from "./signed-authentication-assertion";
import type { SessionAuthorization } from "./session-authorization-port";

export interface BrokerTransaction extends Readonly<Record<string, unknown>> {
  readonly kind: "broker-transaction";
  readonly exp: number;
  readonly transactionId: string;
  readonly stepId: string;
  readonly tenantId: string;
  readonly portalId: string;
  readonly audience: string;
  readonly generationId: string;
  readonly methodId: string;
  readonly returnTo: string;
}

interface BrokerTransactionInput {
  readonly transactionId: string; readonly stepId: string; readonly tenantId: string; readonly portalId: string;
  readonly audience: string; readonly generationId: string; readonly methodId: string; readonly returnTo: string;
}

export function createBrokerTransaction(value: BrokerTransactionInput, expiresAt: string): BrokerTransaction {
  const exp = Math.min(Math.floor(Date.parse(expiresAt) / 1000), nowSeconds() + 600);
  if (!Number.isSafeInteger(exp) || exp <= nowSeconds()) throw new Error("Broker transaction 已过期");
  return Object.freeze({ kind: "broker-transaction", exp, ...value }) as BrokerTransaction;
}

export function parseBrokerTransaction(value: Readonly<Record<string, unknown>>): BrokerTransaction {
  if (value.kind !== "broker-transaction" || !Number.isSafeInteger(value.exp) || !token(value.transactionId) || !token(value.stepId)
    || !safeID(value.tenantId) || !safeID(value.portalId) || typeof value.audience !== "string" || !/^[a-f0-9]{64}$/.test(String(value.generationId))
    || !safeID(value.methodId) || typeof value.returnTo !== "string" || !validReturnTo(value.returnTo)) throw new Error("Broker transaction cookie 无效");
  return value as BrokerTransaction;
}

export function createBrokerSession(signed: SignedAuthenticationAssertion, authorization: SessionAuthorization, maximumAgeSeconds: number): Readonly<Record<string, unknown>> {
	const assertion = signed.payload;
  const expiresAt = Math.min(Date.parse(authorization.expiresAt), Date.now() + maximumAgeSeconds * 1000);
  if (!Number.isFinite(expiresAt) || expiresAt <= Date.now()) throw new Error("Authorization Session 已过期");
  return Object.freeze({
    kind: "broker-session", exp: Math.floor(expiresAt / 1000), sessionId: assertion.assertionId,
    subjectId: authorization.subjectId, tenantId: authorization.tenantId, roles: authorization.roles,
    providerProfileId: assertion.providerProfileId, issuer: assertion.subject.issuer, externalSubject: assertion.subject.id,
    amr: assertion.amr, acr: assertion.acr, policy: authorization.policy, authenticationProof: signed,
  });
}

export function principalFromBrokerSession(value: Readonly<Record<string, unknown>>): Principal {
  if (value.kind !== "broker-session" || !safeID(value.subjectId) || !safeID(value.tenantId) || !Array.isArray(value.roles)
    || value.roles.some((item) => typeof item !== "string")) throw new Error("Broker session 无效");
  return Object.freeze({ id: value.subjectId, tenantId: value.tenantId, roles: Object.freeze([...(value.roles as string[])]) });
}

export function validReturnTo(value: string): boolean { return value.startsWith("/") && !value.startsWith("//") && value.length <= 2048 && !/[\u0000-\u001f\u007f\\]/.test(value); }
function token(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9_-]{32,256}$/.test(value); }
function safeID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
function nowSeconds(): number { return Math.floor(Date.now() / 1000); }
