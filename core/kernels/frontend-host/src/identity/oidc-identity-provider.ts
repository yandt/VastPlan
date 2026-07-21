import { lstat, readFile } from "node:fs/promises";
import type { IncomingMessage, ServerResponse } from "node:http";
import * as oidc from "openid-client";
import type { OIDCIdentityConfig } from "../config/identity-config";
import { appendSetCookie, onlyCookie } from "../http/cookies";
import { sendAPIError, sendJSON } from "../http/json-response";
import { validCSRF } from "../security/csrf";
import type { IdentityProvider, Principal } from "./identity-provider";
import { SessionRejectedError } from "./identity-provider";
import { SealedCookieCodec } from "./sealed-cookie";

const sessionCookie = "vastplan_session";
const transactionCookie = "vastplan_oidc_tx";

export class OIDCIdentityProvider implements IdentityProvider {
  private constructor(private readonly config: OIDCIdentityConfig, private readonly client: oidc.Configuration, private readonly cookies: SealedCookieCodec) {}

  public static async open(config: OIDCIdentityConfig): Promise<OIDCIdentityProvider> {
    const clientSecret = config.clientSecretFile === undefined ? undefined : await readSecret(config.clientSecretFile);
    const authentication = config.clientAuthMethod === "none" ? oidc.None()
      : config.clientAuthMethod === "client_secret_post" ? oidc.ClientSecretPost(clientSecret) : oidc.ClientSecretBasic(clientSecret);
    const client = await oidc.discovery(new URL(config.issuer), config.clientId, {
      redirect_uris: [config.redirectURI], response_types: ["code"], token_endpoint_auth_method: config.clientAuthMethod,
    }, authentication, config.allowInsecure ? { execute: [oidc.allowInsecureRequests] } : undefined);
    if (!client.serverMetadata().supportsPKCE("S256")) throw new Error("OIDC Provider 必须支持 PKCE S256");
    const cookies = await SealedCookieCodec.open(config.sessionKeyFile, `${config.issuer}\0${config.clientId}`);
    return new OIDCIdentityProvider(config, client, cookies);
  }

  public async authenticate(request: IncomingMessage): Promise<Principal> {
    const token = onlyCookie(request, sessionCookie);
    if (token === undefined) throw new SessionRejectedError();
    try { return principalFromSession(this.cookies.unseal(token)); }
    catch { throw new SessionRejectedError(); }
  }

  public loginRedirect(path: string): string { return `/auth/login?returnTo=${encodeURIComponent(validReturnTo(path) ? path : "/")}`; }

  public async handle(request: IncomingMessage, response: ServerResponse, path: string, secureCookies: boolean): Promise<boolean> {
    if (path === "/auth/login") return this.login(request, response, secureCookies);
    if (path === "/auth/callback") return this.callback(request, response, secureCookies);
    if (path === "/auth/logout") return this.logout(request, response, secureCookies);
    if (path === "/auth/session") return this.session(request, response);
    return false;
  }

  private async login(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "GET") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    const state = oidc.randomState(), nonce = oidc.randomNonce(), verifier = oidc.randomPKCECodeVerifier();
    const challenge = await oidc.calculatePKCECodeChallenge(verifier);
    const returnTo = safeReturnTo(request.url);
    const transaction = this.cookies.seal({ kind: "oidc-transaction", exp: nowSeconds() + 300, state, nonce, verifier, returnTo });
    appendSetCookie(response, cookie(transactionCookie, transaction, 300, "/auth/callback", secure, "Lax"));
    const authorization = oidc.buildAuthorizationUrl(this.client, {
      redirect_uri: this.config.redirectURI, scope: this.config.scopes, response_type: "code",
      code_challenge: challenge, code_challenge_method: "S256", state, nonce,
    });
    response.statusCode = 302;
    response.setHeader("Location", authorization.href);
    response.setHeader("Cache-Control", "no-store");
    response.end();
    return true;
  }

  private async callback(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "GET") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    const transactionToken = onlyCookie(request, transactionCookie);
    appendSetCookie(response, clearCookie(transactionCookie, "/auth/callback", secure, "Lax"));
    if (transactionToken === undefined) { sendAPIError(response, 401, "oidc_transaction_required"); return true; }
    let transaction: OIDCTransaction;
    try { transaction = parseTransaction(this.cookies.unseal(transactionToken)); }
    catch { sendAPIError(response, 401, "oidc_transaction_rejected"); return true; }
    try {
      const current = new URL(request.url ?? "/auth/callback", this.config.redirectURI);
      current.protocol = new URL(this.config.redirectURI).protocol;
      current.host = new URL(this.config.redirectURI).host;
      const tokens = await oidc.authorizationCodeGrant(this.client, current, {
        expectedState: transaction.state, expectedNonce: transaction.nonce, pkceCodeVerifier: transaction.verifier, idTokenExpected: true,
      });
      const claims = tokens.claims();
      if (claims === undefined) throw new Error("OIDC Provider 未返回 ID Token");
      const session = sessionFromClaims(claims, this.config);
      appendSetCookie(response, cookie(sessionCookie, this.cookies.seal(session), session.exp - nowSeconds(), "/", secure, "Lax"));
      response.statusCode = 303;
      response.setHeader("Location", transaction.returnTo);
      response.setHeader("Cache-Control", "no-store");
      response.end();
    } catch {
      sendAPIError(response, 401, "oidc_callback_rejected");
    }
    return true;
  }

  private async logout(request: IncomingMessage, response: ServerResponse, secure: boolean): Promise<true> {
    if (request.method !== "POST") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    if (!validCSRF(request)) { sendAPIError(response, 403, "csrf_rejected"); return true; }
    appendSetCookie(response, clearCookie(sessionCookie, "/", secure, "Lax"));
    appendSetCookie(response, clearCookie("vastplan_csrf", "/", secure, "Strict"));
    response.statusCode = 204;
    response.setHeader("Cache-Control", "no-store");
    response.end();
    return true;
  }

  private async session(request: IncomingMessage, response: ServerResponse): Promise<true> {
    if (request.method !== "GET" && request.method !== "HEAD") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    try {
      const principal = await this.authenticate(request);
      sendJSON(response, 200, { authenticated: true, subject: principal.id, tenantId: principal.tenantId, roles: principal.roles }, request.method === "HEAD");
    } catch { sendAPIError(response, 401, "session_required", request.method === "HEAD"); }
    return true;
  }
}

interface OIDCTransaction { readonly state: string; readonly nonce: string; readonly verifier: string; readonly returnTo: string; }
interface OIDCSession extends Readonly<Record<string, unknown>> { readonly kind: "oidc-session"; readonly exp: number; readonly sub: string; readonly tenantId: string; readonly roles: readonly string[]; }

function parseTransaction(value: Readonly<Record<string, unknown>>): OIDCTransaction {
  if (value.kind !== "oidc-transaction" || !safeToken(value.state) || !safeToken(value.nonce) || !safeToken(value.verifier)
    || typeof value.returnTo !== "string" || !validReturnTo(value.returnTo)) throw new Error("OIDC transaction 无效");
  return { state: value.state, nonce: value.nonce, verifier: value.verifier, returnTo: value.returnTo };
}

function sessionFromClaims(claims: Readonly<Record<string, unknown>>, config: OIDCIdentityConfig): OIDCSession {
  if (typeof claims.sub !== "string" || claims.sub === "" || claims.sub.length > 256 || typeof claims.exp !== "number") throw new Error("OIDC Subject 或 expiry 无效");
  const tenantValue = claims[config.tenantClaim] ?? config.defaultTenant;
  const rolesValue = claims[config.rolesClaim];
  if (typeof tenantValue !== "string" || !safeIdentifier(tenantValue) || !Array.isArray(rolesValue) || rolesValue.length > 128
    || rolesValue.some((role) => typeof role !== "string" || !safeIdentifier(role)) || new Set(rolesValue).size !== rolesValue.length) throw new Error("OIDC tenant/roles claim 无效");
  const exp = Math.min(Math.floor(claims.exp), nowSeconds() + config.sessionMaxAgeSeconds);
  if (exp <= nowSeconds()) throw new Error("OIDC ID Token 已过期");
  return Object.freeze({ kind: "oidc-session", exp, sub: claims.sub, tenantId: tenantValue, roles: Object.freeze([...rolesValue]) });
}

function principalFromSession(value: Readonly<Record<string, unknown>>): Principal {
  if (value.kind !== "oidc-session" || typeof value.sub !== "string" || !safeIdentifier(value.tenantId)
    || !Array.isArray(value.roles) || value.roles.some((role) => typeof role !== "string" || !safeIdentifier(role))) throw new Error("OIDC session 无效");
  return Object.freeze({ id: value.sub, tenantId: value.tenantId, roles: Object.freeze([...(value.roles as string[])]) });
}

async function readSecret(path: string): Promise<string> {
  const info = await lstat(path);
  if (!info.isFile() || info.isSymbolicLink() || (info.mode & 0o077) !== 0) throw new Error("OIDC client secret 必须是仅属主可读写的普通文件");
  const secret = (await readFile(path, "utf8")).trim();
  if (secret.length < 16 || secret.length > 4096) throw new Error("OIDC client secret 长度无效");
  return secret;
}
function cookie(name: string, value: string, maxAge: number, path: string, secure: boolean, sameSite: "Lax" | "Strict"): string {
  const parts = [`${name}=${value}`, `Path=${path}`, `Max-Age=${Math.max(0, Math.floor(maxAge))}`, "HttpOnly", `SameSite=${sameSite}`];
  if (secure) parts.push("Secure");
  return parts.join("; ");
}
function clearCookie(name: string, path: string, secure: boolean, sameSite: "Lax" | "Strict"): string { return cookie(name, "", 0, path, secure, sameSite); }
function safeReturnTo(url: string | undefined): string {
  const parsed = new URL(url ?? "/auth/login", "https://portal.invalid");
  const value = parsed.searchParams.get("returnTo") ?? "/";
  return validReturnTo(value) ? value : "/";
}
function validReturnTo(value: string): boolean { return value.startsWith("/") && !value.startsWith("//") && value.length <= 2048 && !/[\u0000-\u001f\u007f\\]/.test(value); }
function safeToken(value: unknown): value is string { return typeof value === "string" && value.length >= 32 && value.length <= 256 && /^[A-Za-z0-9_-]+$/.test(value); }
function safeIdentifier(value: unknown): value is string { return typeof value === "string" && value.length > 0 && value.length <= 256 && /^[A-Za-z0-9._:@/-]+$/.test(value); }
function nowSeconds(): number { return Math.floor(Date.now() / 1000); }
