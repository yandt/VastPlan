import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";

const capability = "platform.artifacts.marketplace";

export class PlatformMarketplaceRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "marketplace") return false;
    const method = request.method ?? "GET";
    if (method !== "GET" && method !== "HEAD") { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
    if (!requirePlatformRole(principal, "platform.artifacts.marketplace.read", response)) return true;
    if (parts.length === 2 && parts[1] === "sources") return this.call("listSources", {}, target, principal, response, signal, method === "HEAD");
    if (parts.length === 2 && parts[1] === "catalog") {
      let query: URLSearchParams;
      try { query = new URL(request.url ?? "", "http://portal.invalid").searchParams; }
      catch { sendAPIError(response, 400, "invalid_marketplace_query", method === "HEAD"); return true; }
      const payload = catalogRequest(query);
      if (payload === undefined) { sendAPIError(response, 400, "invalid_marketplace_query", method === "HEAD"); return true; }
      return this.call("listCatalog", payload, target, principal, response, signal, method === "HEAD");
    }
    sendAPIError(response, 404, "not_found", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, target: PlatformManagementTarget, principal: Principal, response: ServerResponse, signal: AbortSignal, head: boolean): Promise<true> {
    if (!authorizePlatformOperation(this.client, target, capability, operation, false, response)) return true;
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: false, payload, response, signal, head });
    return true;
  }
}

function catalogRequest(params: URLSearchParams): Record<string, unknown> | undefined {
  const allowed = new Set(["sourceId", "pluginId", "pluginPrefix", "namespace", "publisher", "version", "channel", "target", "lifecycle", "page", "pageSize"]);
  if ([...params.keys()].some((key) => !allowed.has(key) || params.getAll(key).length !== 1)) return undefined;
  const sourceId = params.get("sourceId");
  if (sourceId === null || !/^[a-z][a-z0-9._-]{0,127}$/.test(sourceId)) return undefined;
  const page = positiveInteger(params.get("page"), 1, Number.MAX_SAFE_INTEGER, 1);
  const pageSize = positiveInteger(params.get("pageSize"), 1, 100, 20);
  if (page === undefined || pageSize === undefined) return undefined;
  const query: Record<string, unknown> = { page, pageSize };
  for (const key of ["pluginId", "pluginPrefix", "namespace", "publisher", "version", "channel", "target", "lifecycle"] as const) {
    const value = params.get(key);
    if (value !== null) {
      if (value.length === 0 || value.length > 255 || /[\0\r\n]/.test(value)) return undefined;
      query[key] = value;
    }
  }
  return { version: 1, sourceId, query };
}

function positiveInteger(raw: string | null, minimum: number, maximum: number, fallback: number): number | undefined {
  if (raw === null) return fallback;
  if (!/^[1-9][0-9]*$/.test(raw)) return undefined;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value >= minimum && value <= maximum ? value : undefined;
}
