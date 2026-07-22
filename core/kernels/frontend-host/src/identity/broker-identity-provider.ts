import { createHash, randomBytes } from "node:crypto";
import type { IncomingMessage, ServerResponse } from "node:http";
import type { AccessCatalogPort } from "../access/access-catalog-port";
import type { BrokerIdentityConfig } from "../config/identity-config";
import { appendSetCookie, onlyCookie } from "../http/cookies";
import { sendAPIError, sendJSON } from "../http/json-response";
import { readRequestJSON, requireJSONObject, RequestJSONError } from "../http/request-json";
import { issueCSRF, validCSRF } from "../security/csrf";
import type { AuthenticationBrokerPort } from "./authentication-broker-port";
import { parseContinueInput, parseDescribe, parseResultEnvelope } from "./broker-identity-contract";
import { createBrokerSession, createBrokerTransaction, parseBrokerTransaction, principalFromBrokerSession, validReturnTo } from "./broker-identity-session";
import type { IdentityProvider, Principal } from "./identity-provider";
import { SessionRejectedError } from "./identity-provider";
import type { SessionAuthorizationPort } from "./session-authorization-port";
import { AuthenticationAssertionVerifier, parseSignedAuthenticationAssertion } from "./signed-authentication-assertion";
import { SealedCookieCodec } from "./sealed-cookie";

const sessionCookie = "vastplan_session";
const transactionCookie = "vastplan_auth_tx";
const providerTestCookie = "vastplan_auth_test";

export class BrokerIdentityProvider implements IdentityProvider {
  private constructor(
    private readonly config: BrokerIdentityConfig,
    private readonly access: AccessCatalogPort,
    private readonly broker: AuthenticationBrokerPort,
    private readonly authorization: SessionAuthorizationPort,
    private readonly assertions: AuthenticationAssertionVerifier,
    private readonly cookies: SealedCookieCodec,
  ) {}

  public static async open(config: BrokerIdentityConfig, access: AccessCatalogPort, broker: AuthenticationBrokerPort, authorization: SessionAuthorizationPort): Promise<BrokerIdentityProvider> {
    const [assertions, cookies] = await Promise.all([
      AuthenticationAssertionVerifier.open(config.assertionTrustFile),
      SealedCookieCodec.open(config.sessionKeyFile, "vastplan.portal.broker-session.v1"),
    ]);
    return new BrokerIdentityProvider(config, access, broker, authorization, assertions, cookies);
  }

  public async authenticate(request: IncomingMessage): Promise<Principal> {
    const token = onlyCookie(request, sessionCookie);
    if (token === undefined) throw new SessionRejectedError();
    try { return principalFromBrokerSession(this.cookies.unseal(token)); }
    catch { throw new SessionRejectedError(); }
  }

  public async authenticationProof(request: IncomingMessage) {
    const token = onlyCookie(request, sessionCookie);
    if (token === undefined) return undefined;
    try { return parseSignedAuthenticationAssertion(this.cookies.unseal(token).authenticationProof); }
    catch { return undefined; }
  }

  public async authenticationTestProof(request: IncomingMessage) {
    const token = onlyCookie(request, providerTestCookie);
    if (token === undefined) return undefined;
    try {
      const value = this.cookies.unseal(token);
      return value.kind === "provider-test-proof" ? parseSignedAuthenticationAssertion(value.assertion) : undefined;
    } catch { return undefined; }
  }

  public loginRedirect(path: string): string { return `/auth/access?returnTo=${encodeURIComponent(validReturnTo(path) ? path : "/")}`; }

  public async handle(request: IncomingMessage, response: ServerResponse, path: string, secure: boolean): Promise<boolean> {
    if (path === "/auth/login") return this.login(request, response);
    if (path === "/auth/callback") return this.callback(request, response, secure);
    if (path === "/auth/logout") return this.logout(request, response, secure);
    if (path === "/auth/session") return this.session(request, response);
    if (path === "/auth/v1/csrf") return this.csrf(request, response, secure);
    if (path === "/auth/v1/methods") return this.methods(request, response);
    if (path === "/auth/v1/transactions") return this.begin(request, response, secure);
    if (path === "/auth/v1/provider-tests") return this.beginProviderTest(request, response, secure);
    const match = /^\/auth\/v1\/transactions\/([A-Za-z0-9_-]{32,256})(?:\/(continue|resend))?$/.exec(path);
    if (match !== null) {
      if (match[2] === undefined && request.method === "DELETE") return this.cancel(request, response, secure, match[1]);
      return match[2] === "resend" ? this.resend(request, response, secure, match[1]) : this.continue(request, response, secure, match[1]);
    }
    return false;
  }

  private async login(request: IncomingMessage, response: ServerResponse): Promise<true> {
    if (request.method !== "GET") return apiError(response, 405, "method_not_allowed");
    const returnTo = queryReturnTo(request.url);
    response.statusCode = 303;
    response.setHeader("Location", `/auth/access?returnTo=${encodeURIComponent(returnTo)}`);
    response.setHeader("Cache-Control", "no-store");
    response.end();
    return true;
  }

  private async csrf(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "GET") return apiError(response, 405, "method_not_allowed");
    sendJSON(response, 200, { token: issueCSRF(response, secure) });
    return true;
  }

  private async methods(request: IncomingMessage, response: ServerResponse): Promise<true> {
    if (request.method !== "GET") return apiError(response, 405, "method_not_allowed");
    const target = await this.target(request, queryReturnTo(request.url));
    if (target === undefined) return apiError(response, 404, "access_profile_not_found");
    const methods = parseDescribe(await this.broker.call(target.profile.tenantId, "describe", { tenantId: target.profile.tenantId, portalId: target.profile.portalId }));
    const allowed = new Set(target.profile.authentication.allowedMethods);
    sendJSON(response, 200, { methods: methods.filter(({ methodId }) => allowed.has(methodId)), defaultMethod: target.profile.authentication.defaultMethod });
    return true;
  }

  private async begin(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "POST") return apiError(response, 405, "method_not_allowed");
    if (!validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    try {
      const body = requireJSONObject(await readRequestJSON(request, 8192));
      if (!hasOnlyFrom(body, ["methodId", "locale", "returnTo"]) || !safeID(body.methodId) || typeof body.locale !== "string" || body.locale.length > 64 || typeof body.returnTo !== "string" || !validReturnTo(body.returnTo)) throw new RequestJSONError("认证事务请求无效");
      const target = await this.target(request, body.returnTo);
      if (target === undefined || !target.profile.authentication.allowedMethods.includes(body.methodId)) return apiError(response, 404, "authentication_method_not_found");
      const transactionId = randomBytes(24).toString("base64url"), audience = targetAudience(request, target.profile.portalId);
      const result = parseResultEnvelope(await this.broker.call(target.profile.tenantId, "begin", {
        transactionId, methodId: body.methodId, audience, tenantId: target.profile.tenantId, portalId: target.profile.portalId,
        locale: body.locale, clientContextDigest: clientContextDigest(request),
      }), false).result;
      if (result.state !== "challenge" || result.step === undefined) throw new Error("Broker begin 未返回 challenge");
      const transaction = createBrokerTransaction({ transactionId, stepId: result.step.stepId, tenantId: target.profile.tenantId, portalId: target.profile.portalId, audience, generationId: target.id, methodId: body.methodId, returnTo: body.returnTo, purpose: "login" }, result.step.expiresAt);
      appendSetCookie(response, cookie(transactionCookie, this.cookies.seal(transaction), transaction.exp - nowSeconds(), "/auth", secure, "Lax"));
      sendJSON(response, 201, { transactionId, result });
    } catch (error) { if (error instanceof RequestJSONError) return apiError(response, 400, "invalid_authentication_request"); throw error; }
    return true;
  }

  private async beginProviderTest(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "POST") return apiError(response, 405, "method_not_allowed");
    if (!validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    let principal: Principal;
    try { principal = await this.authenticate(request); } catch { return apiError(response, 401, "session_required"); }
    if (!principal.roles.includes("foundation.security.authentication.providers.test")) return apiError(response, 403, "forbidden");
    try {
      const body = requireJSONObject(await readRequestJSON(request, 8192));
      if (!hasOnlyFrom(body, ["providerProfileId", "methodId", "locale", "returnTo"]) || !safeID(body.providerProfileId) || !safeID(body.methodId) || typeof body.locale !== "string" || body.locale.length > 64 || typeof body.returnTo !== "string" || !validReturnTo(body.returnTo)) throw new RequestJSONError("Provider 测试请求无效");
      const target = await this.target(request, body.returnTo);
      if (target === undefined) return apiError(response, 404, "access_profile_not_found");
      const transactionId = randomBytes(24).toString("base64url");
      const result = parseResultEnvelope(await this.broker.call(target.profile.tenantId, "beginProviderTest", { transactionId, providerProfileId: body.providerProfileId, methodId: body.methodId, tenantId: target.profile.tenantId, portalId: target.profile.portalId, locale: body.locale, clientContextDigest: clientContextDigest(request) }), false).result;
      if (result.state !== "challenge" || result.step === undefined) throw new Error("Provider 测试未返回 challenge");
      const transaction = createBrokerTransaction({ transactionId, stepId: result.step.stepId, tenantId: target.profile.tenantId, portalId: target.profile.portalId, audience: "authentication-provider-test", generationId: target.id, methodId: body.methodId, returnTo: body.returnTo, purpose: "provider-test" }, result.step.expiresAt);
      appendSetCookie(response, cookie(transactionCookie, this.cookies.seal(transaction), transaction.exp - nowSeconds(), "/auth", secure, "Lax"));
      sendJSON(response, 201, { transactionId, result });
    } catch (error) { if (error instanceof RequestJSONError) return apiError(response, 400, "invalid_provider_test_request"); throw error; }
    return true;
  }

  private async continue(request: IncomingMessage, response: ServerResponse, secure: boolean, id: string, redirect?: Readonly<Record<string, string>>): Promise<true> {
    if (redirect === undefined && request.method !== "POST") return apiError(response, 405, "method_not_allowed");
    if (redirect === undefined && !validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    const transaction = this.readTransaction(request, id);
    if (transaction === undefined) return apiError(response, 401, "authentication_transaction_rejected");
    try {
      const input = redirect === undefined ? parseContinueInput(await readRequestJSON(request, 16 << 10)) : { stepId: transaction.stepId, redirect };
      if (input.stepId !== transaction.stepId) return apiError(response, 409, "authentication_step_mismatch");
      const value = parseResultEnvelope(await this.broker.call(transaction.tenantId, "continue", { transactionId: id, ...input }), true);
      if (value.result.state !== "authenticated") {
        if (value.result.step !== undefined) this.updateTransactionCookie(response, transaction, value.result.step.stepId, value.result.step.expiresAt, secure);
        sendJSON(response, 200, { transactionId: id, result: value.result });
        return true;
      }
      const signed = this.assertions.verify(value.assertion, transaction);
      const consumed = await this.broker.call(transaction.tenantId, "consumeAssertion", { assertion: signed, audience: transaction.audience, tenantId: transaction.tenantId, portalId: transaction.portalId, transactionId: transaction.transactionId });
      if (!isRecord(consumed) || consumed.consumed !== true) throw new Error("Broker 未确认 Assertion 消费");
      if (transaction.purpose === "provider-test") {
        const proof = { kind: "provider-test-proof", exp: Math.floor(Date.parse(signed.payload.expiresAt) / 1000), assertion: signed };
        appendSetCookie(response, clearCookie(transactionCookie, "/auth", secure, "Lax"));
        appendSetCookie(response, cookie(providerTestCookie, this.cookies.seal(proof), Number(proof.exp) - nowSeconds(), "/", secure, "Strict"));
        sendJSON(response, 200, { transactionId: id, result: { state: "authenticated" }, returnTo: transaction.returnTo });
        return true;
      }
      const authorization = await this.authorization.resolve(signed.payload);
      const session = createBrokerSession(signed, authorization, this.config.sessionMaxAgeSeconds);
      appendSetCookie(response, clearCookie(transactionCookie, "/auth", secure, "Lax"));
      appendSetCookie(response, cookie(sessionCookie, this.cookies.seal(session), Number(session.exp) - nowSeconds(), "/", secure, "Lax"));
      if (redirect === undefined) sendJSON(response, 200, { transactionId: id, result: { state: "authenticated" }, returnTo: transaction.returnTo });
      else { response.statusCode = 303; response.setHeader("Location", transaction.returnTo); response.setHeader("Cache-Control", "no-store"); response.end(); }
    } catch (error) {
      appendSetCookie(response, clearCookie(transactionCookie, "/auth", secure, "Lax"));
      if (error instanceof RequestJSONError) return apiError(response, 400, "invalid_authentication_response");
      throw error;
    }
    return true;
  }

  private async callback(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "GET") return apiError(response, 405, "method_not_allowed");
    const transaction = this.readTransaction(request);
    if (transaction === undefined) return apiError(response, 401, "authentication_transaction_rejected");
    let url: URL;
    try { url = new URL(request.url ?? "/auth/callback", "https://portal.invalid"); } catch { return apiError(response, 400, "invalid_authentication_callback"); }
    const state = url.searchParams.get("state"), code = url.searchParams.get("code"), error = url.searchParams.get("error"), errorDescription = url.searchParams.get("error_description");
    if (state === null || ((code === null) === (error === null))) return apiError(response, 400, "invalid_authentication_callback");
    const redirect = Object.freeze({ state, ...(code === null ? { error: error!, ...(errorDescription === null ? {} : { errorDescription }) } : { code }) });
    return this.continue(request, response, secure, transaction.transactionId, redirect);
  }

  private async resend(request: IncomingMessage, response: ServerResponse, secure: boolean, id: string): Promise<true> {
    if (request.method !== "POST") return apiError(response, 405, "method_not_allowed");
    if (!validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    const transaction = this.readTransaction(request, id);
    if (transaction === undefined) return apiError(response, 401, "authentication_transaction_rejected");
    const value = parseResultEnvelope(await this.broker.call(transaction.tenantId, "resend", { transactionId: id, stepId: transaction.stepId }), false);
    if (value.result.step !== undefined) this.updateTransactionCookie(response, transaction, value.result.step.stepId, value.result.step.expiresAt, secure);
    sendJSON(response, 200, { transactionId: id, result: value.result });
    return true;
  }

  private async cancel(request: IncomingMessage, response: ServerResponse, secure: boolean, id: string): Promise<true> {
    if (!validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    const transaction = this.readTransaction(request, id);
    if (transaction === undefined) return apiError(response, 401, "authentication_transaction_rejected");
    await this.broker.call(transaction.tenantId, "cancel", { transactionId: id });
    appendSetCookie(response, clearCookie(transactionCookie, "/auth", secure, "Lax"));
    response.statusCode = 204; response.setHeader("Cache-Control", "no-store"); response.end();
    return true;
  }

  private async logout(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "POST") return apiError(response, 405, "method_not_allowed");
    if (!validMutation(request, secure)) return apiError(response, 403, "csrf_rejected");
    appendSetCookie(response, clearCookie(sessionCookie, "/", secure, "Lax"));
    appendSetCookie(response, clearCookie("vastplan_csrf", "/", secure, "Strict"));
    response.statusCode = 204; response.setHeader("Cache-Control", "no-store"); response.end();
    return true;
  }

  private async session(request: IncomingMessage, response: ServerResponse): Promise<true> {
    if (request.method !== "GET" && request.method !== "HEAD") return apiError(response, 405, "method_not_allowed");
    try { const principal = await this.authenticate(request); sendJSON(response, 200, { authenticated: true, subject: principal.id, tenantId: principal.tenantId, roles: principal.roles }, request.method === "HEAD"); }
    catch { return apiError(response, 401, "session_required", request.method === "HEAD"); }
    return true;
  }

  private readTransaction(request: IncomingMessage, expectedID?: string) {
    const token = onlyCookie(request, transactionCookie);
    if (token === undefined) return undefined;
    try { const value = parseBrokerTransaction(this.cookies.unseal(token)); return expectedID === undefined || value.transactionId === expectedID ? value : undefined; }
    catch { return undefined; }
  }

  private updateTransactionCookie(response: ServerResponse, transaction: ReturnType<typeof parseBrokerTransaction>, stepId: string, expiresAt: string, secure: boolean): void {
    const updated = createBrokerTransaction({ ...transaction, stepId }, expiresAt);
    appendSetCookie(response, cookie(transactionCookie, this.cookies.seal(updated), updated.exp - nowSeconds(), "/auth", secure, "Lax"));
  }

  private async target(request: IncomingMessage, returnTo: string) {
    const host = requestHost(request);
    return host === undefined ? undefined : this.access.resolve(host, new URL(returnTo, "https://portal.invalid").pathname);
  }
}

function validMutation(request: IncomingMessage, secure: boolean): boolean {
  if (!validCSRF(request)) return false;
  const host = request.headers.host, origin = request.headers.origin, fetchSite = request.headers["sec-fetch-site"];
  if (typeof host !== "string" || typeof origin !== "string" || origin !== `${secure ? "https" : "http"}://${host}`) return false;
  return fetchSite === undefined || fetchSite === "same-origin";
}
function requestHost(request: IncomingMessage): string | undefined { try { const value = new URL(`https://${request.headers.host ?? ""}`); return value.hostname.toLowerCase().replace(/\.$/, ""); } catch { return undefined; } }
function targetAudience(request: IncomingMessage, portalId: string): string { const host = requestHost(request); if (host === undefined) throw new RequestJSONError("Host 无效"); return `portal:${host}:${portalId}`; }
function clientContextDigest(request: IncomingMessage): string { return createHash("sha256").update(`${request.headers["user-agent"] ?? ""}\0${request.headers["accept-language"] ?? ""}`).digest("hex"); }
function queryReturnTo(raw: string | undefined): string { try { const value = new URL(raw ?? "/", "https://portal.invalid").searchParams.get("returnTo") ?? "/"; return validReturnTo(value) ? value : "/"; } catch { return "/"; } }
function cookie(name: string, value: string, maxAge: number, path: string, secure: boolean, sameSite: "Lax" | "Strict"): string { const parts = [`${name}=${value}`, `Path=${path}`, `Max-Age=${Math.max(0, Math.floor(maxAge))}`, "HttpOnly", `SameSite=${sameSite}`]; if (secure) parts.push("Secure"); return parts.join("; "); }
function clearCookie(name: string, path: string, secure: boolean, sameSite: "Lax" | "Strict"): string { return cookie(name, "", 0, path, secure, sameSite); }
function apiError(response: ServerResponse, status: number, code: string, head = false): true { sendAPIError(response, status, code, head); return true; }
function safeID(value: unknown): value is string { return typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9._:@/-]{0,255}$/.test(value); }
function hasOnlyFrom(value: Readonly<Record<string, unknown>>, keys: readonly string[]): boolean { return Object.keys(value).every((key) => keys.includes(key)); }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function nowSeconds(): number { return Math.floor(Date.now() / 1000); }
