import type { IncomingMessage, ServerResponse } from "node:http";
import Ajv2020, { type ValidateFunction } from "ajv/dist/2020.js";
import { CapabilityApplicationError, type TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import type { IdentityProvider, Principal } from "../identity/identity-provider";
import { validCSRF } from "../security/csrf";
import { sendAPIError, sendJSON } from "../http/json-response";
import type { APIExposureCatalogPort, APIRouteContract, GatewayInvocation, ResolvedAPIExposure } from "./api-exposure-contract";
import { matchAPIRoute } from "./api-route-matcher";
import { APIExposureRateLimiter } from "./api-rate-limiter";

const publicPath = /^\/api\/r\/([a-z2-7]{20})\/v([1-9][0-9]*)(\/.*)$/;
const maximumQueryKeys = 64;
const maximumQueryValues = 32;
const maximumQueryValueBytes = 4_096;

export class APIExposureGateway {
  private readonly rateLimiter: APIExposureRateLimiter;
  private readonly validators = new Map<string, { request: ValidateFunction; response: ValidateFunction }>();

  public constructor(
    private readonly catalog: APIExposureCatalogPort,
    private readonly identity: IdentityProvider,
    private readonly invoker: TrustedCapabilityInvoker,
    private readonly secureCookies: boolean,
    rateLimiter?: APIExposureRateLimiter,
  ) {
    this.rateLimiter = rateLimiter ?? new APIExposureRateLimiter();
  }

  public async handle(request: IncomingMessage, response: ServerResponse, path: string): Promise<void> {
    const publicMatch = publicPath.exec(path);
    if (publicMatch === null) return sendAPIError(response, 404, "not_found");
    const host = request.headers.host;
    if (host === undefined) return sendAPIError(response, 400, "invalid_host");
    const major = Number(publicMatch[2]);
    if (!Number.isSafeInteger(major)) return sendAPIError(response, 404, "not_found");
    const resolved = await this.catalog.resolve(host, publicMatch[1], major);
    if (resolved === undefined) return sendAPIError(response, 404, "not_found");
    const method = request.method ?? "GET";
    const matched = matchAPIRoute(resolved.contract, method, publicMatch[3]);
    if (matched === "method-not-allowed") return sendAPIError(response, 405, "method_not_allowed");
    if (matched === undefined) return sendAPIError(response, 404, "not_found");

    const principal = await this.authenticate(request, resolved);
    if (principal === undefined) return sendAPIError(response, 401, "authentication_required");
    if (!this.authorizedPrincipal(principal, resolved)) return sendAPIError(response, 403, "forbidden");
    if (method !== "GET" && !resolved.exposure.authentication.allowAnonymous && !validCSRF(request)) {
      return sendAPIError(response, 403, "csrf_rejected");
    }
    if (!this.rateLimiter.allow(resolved.exposure.routeKey, principal.id, resolved.exposure.limits.requestsPerMinute)) {
      response.setHeader("Retry-After", "60");
      return sendAPIError(response, 429, "rate_limited");
    }

    let body: unknown;
    let query: Readonly<Record<string, readonly string[]>>;
    try {
      body = await readJSONBody(request, resolved.exposure.limits.maxBodyBytes, method);
      query = parseQuery(request.url ?? path);
    } catch (error) {
      return sendAPIError(response, error instanceof UnsupportedMediaTypeError ? 415 : error instanceof BodyTooLargeError ? 413 : 400,
        error instanceof UnsupportedMediaTypeError ? "unsupported_media_type" : error instanceof BodyTooLargeError ? "body_too_large" : "invalid_request");
    }

    const validators = this.routeValidators(resolved, matched.route);
    if (!validators.request(body)) return sendAPIError(response, 422, "request_schema_rejected");
    const invocation: GatewayInvocation = {
      schemaVersion: "v1", routeId: matched.route.id, method: matched.route.method,
      pathParams: matched.pathParams, query, body,
    };
    const clientAbort = new AbortController();
    request.once("aborted", () => clientAbort.abort(new Error("Client aborted")));
    const signal = AbortSignal.any([clientAbort.signal, AbortSignal.timeout(resolved.exposure.limits.timeoutMs)]);
    try {
      const raw = await this.invoker.invoke(principal, {
        capability: matched.route.target.capability,
        logicalService: resolved.exposure.target.logicalService,
        routingDomain: resolved.exposure.target.routingDomain,
      }, matched.route.target.operation, Buffer.from(JSON.stringify(invocation)), signal);
      if (raw.byteLength > resolved.exposure.limits.maxResponseBytes) {
        reportGatewayFailure(resolved, matched.route, "response_too_large");
        return sendAPIError(response, 502, "upstream_invalid_response");
      }
      let value: unknown;
      try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
      catch {
        reportGatewayFailure(resolved, matched.route, "response_not_json");
        return sendAPIError(response, 502, "upstream_invalid_response");
      }
      if (!validators.response(value)) {
        reportGatewayFailure(resolved, matched.route, "response_schema_rejected");
        return sendAPIError(response, 502, "upstream_invalid_response");
      }
      if (matched.route.successStatus === 204) {
        response.statusCode = 204;
        response.end();
        return;
      }
      sendJSON(response, matched.route.successStatus, value);
    } catch (error) {
      if (error instanceof CapabilityApplicationError) return sendCapabilityError(response, matched.route, error.code);
      if (signal.aborted) return sendAPIError(response, 504, "upstream_timeout");
      reportGatewayFailure(resolved, matched.route, "transport_failed");
      sendAPIError(response, 502, "upstream_unavailable");
    }
  }

  private async authenticate(request: IncomingMessage, resolved: ResolvedAPIExposure): Promise<Principal | undefined> {
    try { return await this.identity.authenticate(request); }
    catch {
      if (!resolved.exposure.authentication.allowAnonymous) return undefined;
      return Object.freeze({ id: "anonymous", tenantId: resolved.exposure.tenantId, roles: Object.freeze([]) });
    }
  }

  private authorizedPrincipal(principal: Principal, resolved: ResolvedAPIExposure): boolean {
    const exposure = resolved.exposure;
    if (principal.tenantId !== exposure.tenantId) return false;
    if (exposure.portalId !== undefined && principal.portalId !== exposure.portalId) return false;
    if (principal.id !== "anonymous" && principal.authenticationProfileId !== exposure.authentication.profileId) return false;
    return exposure.requiredPermissions.every((permission) => principal.roles.includes(permission));
  }

  private routeValidators(resolved: ResolvedAPIExposure, route: APIRouteContract): { request: ValidateFunction; response: ValidateFunction } {
    const key = `${resolved.exposure.contract.contractDigest}\0${route.id}`;
    const cached = this.validators.get(key);
    if (cached !== undefined) return cached;
    const ajv = new Ajv2020({ allErrors: false, strict: true });
    const compiled = { request: ajv.compile(route.requestSchema), response: ajv.compile(route.responseSchema) };
    if (this.validators.size >= 10_000) this.validators.clear();
    this.validators.set(key, compiled);
    return compiled;
  }
}

async function readJSONBody(request: IncomingMessage, maximumBytes: number, method: string): Promise<unknown> {
  const contentLength = request.headers["content-length"];
  const declared = contentLength === undefined ? undefined : Number(contentLength);
  if (declared !== undefined && (!Number.isSafeInteger(declared) || declared < 0)) throw new Error("Content-Length 无效");
  if (declared !== undefined && declared > maximumBytes) throw new BodyTooLargeError();
  if (method === "GET" && ((declared ?? 0) > 0 || request.headers["transfer-encoding"] !== undefined)) throw new Error("GET 不得包含请求体");
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as Uint8Array);
    size += bytes.byteLength;
    if (size > maximumBytes) throw new BodyTooLargeError();
    chunks.push(bytes);
  }
  if (size === 0) return {};
  const contentType = request.headers["content-type"]?.split(";", 1)[0].trim().toLowerCase();
  if (contentType !== "application/json") throw new UnsupportedMediaTypeError();
  try { return JSON.parse(Buffer.concat(chunks, size).toString("utf8")) as unknown; }
  catch { throw new Error("请求 JSON 无效"); }
}

function parseQuery(rawURL: string): Readonly<Record<string, readonly string[]>> {
  const url = new URL(rawURL, "https://gateway.invalid");
  const result: Record<string, string[]> = {};
  for (const [key, value] of url.searchParams) {
    if (!/^[a-z][A-Za-z0-9._-]*$/.test(key) || Buffer.byteLength(key) > 160 || Buffer.byteLength(value) > maximumQueryValueBytes) throw new Error("query 超过上限");
    const values = result[key] ?? [];
    if (values.length >= maximumQueryValues) throw new Error("query 重复值超过上限");
    values.push(value);
    result[key] = values;
  }
  if (Object.keys(result).length > maximumQueryKeys) throw new Error("query key 超过上限");
  return Object.freeze(Object.fromEntries(Object.entries(result).map(([key, values]) => [key, Object.freeze(values)])));
}

function sendCapabilityError(response: ServerResponse, route: APIRouteContract, code: string): void {
  if (code === "permission.denied") return sendAPIError(response, 403, "forbidden");
  const mapping = route.errors?.find((candidate) => candidate.code === code);
  if (mapping === undefined) return sendAPIError(response, 502, "upstream_rejected");
  sendAPIError(response, mapping.status, mapping.code);
}

function reportGatewayFailure(resolved: ResolvedAPIExposure, route: APIRouteContract, reason: string): void {
  process.stderr.write(`${JSON.stringify({
    level: "error", message: "api exposure gateway failure", exposure_id: resolved.exposure.id,
    route_id: route.id, reason,
  })}\n`);
}

class BodyTooLargeError extends Error {}
class UnsupportedMediaTypeError extends Error {}
