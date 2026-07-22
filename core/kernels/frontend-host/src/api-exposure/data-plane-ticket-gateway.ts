import type { IncomingMessage, ServerResponse } from "node:http";
import { CapabilityApplicationError, type TrustedCapabilityInvoker } from "../capabilities/capability-invoker";
import { sendAPIError, sendJSON } from "../http/json-response";
import type { IdentityProvider, Principal } from "../identity/identity-provider";
import { validCSRF } from "../security/csrf";
import type { APIExposureCatalogPort, DataPlaneExposure } from "./api-exposure-contract";

const maximumRequestBytes = 8 << 10;
const ticketPattern = /^[A-Za-z0-9_-]{43}$/;
const sha256Pattern = /^[a-f0-9]{64}$/;

interface TicketRequest {
  method: "GET" | "PUT";
  resource: string;
  contentSha256?: string;
}

interface TicketGrant {
  endpoint: string;
  leaseId: string;
  ticket: string;
  expiresAt: string;
}

export class DataPlaneTicketGateway {
  public constructor(
    private readonly catalog: APIExposureCatalogPort,
    private readonly identity: IdentityProvider,
    private readonly invoker: TrustedCapabilityInvoker,
  ) {}

  public async handle(request: IncomingMessage, response: ServerResponse, routeKey: string): Promise<void> {
    if ((request.method ?? "GET") !== "POST") return sendAPIError(response, 405, "method_not_allowed");
    const host = request.headers.host;
    if (host === undefined) return sendAPIError(response, 400, "invalid_host");
    const exposure = await this.catalog.resolveDataPlane(host, routeKey);
    if (exposure === undefined || !exposure.allowedModes.includes("ticket-redirect")) return sendAPIError(response, 404, "not_found");
    const principal = await this.authenticate(request, exposure);
    if (principal === undefined) return sendAPIError(response, 401, "authentication_required");
    if (!authorized(principal, exposure)) return sendAPIError(response, 403, "forbidden");
    if (principal.requiresCSRF !== false && !exposure.authentication.allowAnonymous && !validCSRF(request)) return sendAPIError(response, 403, "csrf_rejected");

    let body: TicketRequest;
    try { body = validateTicketRequest(await readJSON(request)); }
    catch { return sendAPIError(response, 400, "invalid_request"); }
    try {
      const raw = await this.invoker.invoke(principal, {
        capability: "platform.api-exposure",
        logicalService: "platform.api-exposure",
        routingDomain: "platform",
      }, "issueDataPlaneTicket", Buffer.from(JSON.stringify({ dataPlaneExposureId: exposure.id, ...body })));
      const grant = validateTicketGrant(JSON.parse(new TextDecoder().decode(raw)) as unknown);
      response.setHeader("Cache-Control", "no-store");
      sendJSON(response, 200, grant);
    } catch (error) {
      if (error instanceof CapabilityApplicationError && error.code === "permission.denied") return sendAPIError(response, 403, "forbidden");
      return sendAPIError(response, 503, "data_plane_unavailable");
    }
  }

  private async authenticate(request: IncomingMessage, exposure: DataPlaneExposure): Promise<Principal | undefined> {
    try { return await this.identity.authenticate(request); }
    catch {
      if (!exposure.authentication.allowAnonymous) return undefined;
      return Object.freeze({ id: "anonymous", tenantId: exposure.tenantId, roles: Object.freeze([]) });
    }
  }
}

function authorized(principal: Principal, exposure: DataPlaneExposure): boolean {
  return principal.tenantId === exposure.tenantId
    && (principal.id === "anonymous" || principal.authenticationProfileId === exposure.authentication.profileId)
    && exposure.requiredPermissions.every((permission) => principal.roles.includes(permission));
}

async function readJSON(request: IncomingMessage): Promise<unknown> {
  const contentType = request.headers["content-type"]?.split(";", 1)[0].trim().toLowerCase();
  if (contentType !== "application/json") throw new Error("Content-Type 无效");
  const declared = request.headers["content-length"] === undefined ? undefined : Number(request.headers["content-length"]);
  if (declared !== undefined && (!Number.isSafeInteger(declared) || declared < 0 || declared > maximumRequestBytes)) throw new Error("请求过大");
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of request) {
    const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk as Uint8Array);
    size += bytes.byteLength;
    if (size > maximumRequestBytes) throw new Error("请求过大");
    chunks.push(bytes);
  }
  return JSON.parse(Buffer.concat(chunks, size).toString("utf8")) as unknown;
}

function validateTicketRequest(value: unknown): TicketRequest {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("请求必须是对象");
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some((key) => !["method", "resource", "contentSha256"].includes(key))) throw new Error("请求字段无效");
  if (record.method !== "GET" && record.method !== "PUT") throw new Error("方法无效");
  if (typeof record.resource !== "string" || !record.resource.startsWith("/") || record.resource.startsWith("//") || record.resource.length > 2_048 || record.resource.includes("\\") || /[\r\n\0]/.test(record.resource)) throw new Error("资源无效");
  if (record.contentSha256 !== undefined && (typeof record.contentSha256 !== "string" || !sha256Pattern.test(record.contentSha256))) throw new Error("摘要无效");
  return { method: record.method, resource: record.resource, ...(record.contentSha256 === undefined ? {} : { contentSha256: record.contentSha256 as string }) };
}

function validateTicketGrant(value: unknown): TicketGrant {
  if (typeof value !== "object" || value === null || Array.isArray(value)) throw new Error("Ticket 响应无效");
  const record = value as Record<string, unknown>;
  if (typeof record.endpoint !== "string" || typeof record.leaseId !== "string" || typeof record.ticket !== "string" || typeof record.expiresAt !== "string") throw new Error("Ticket 响应无效");
  const endpoint = new URL(record.endpoint);
  const expiresAt = Date.parse(record.expiresAt);
  const now = Date.now();
  if (endpoint.protocol !== "https:" || endpoint.username !== "" || endpoint.password !== "" || endpoint.search !== "" || endpoint.hash !== "" || !ticketPattern.test(record.ticket) || !Number.isFinite(expiresAt) || expiresAt <= now || expiresAt > now + 35_000) throw new Error("Ticket 响应无效");
  return { endpoint: record.endpoint, leaseId: record.leaseId, ticket: record.ticket, expiresAt: record.expiresAt };
}
