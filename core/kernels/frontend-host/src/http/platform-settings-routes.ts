import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { optionalNonnegativeVersion, queryValue, requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.settings";

export class PlatformSettingsRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "settings") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1) {
      if (!authorizePlatformOperation(this.client, target, capability, "list", false, response) || !requirePlatformRole(principal, "platform.settings.read", response)) return true;
      if (method !== "GET" && method !== "HEAD") return methodNotAllowed(response, method);
      await this.call(principal, target, "list", false, { prefix: queryValue(request.url, "prefix") }, response, signal, method === "HEAD", listItems);
      return true;
    }
    if (parts.length !== 2) return notFound(response, method);
    const key = resourceName(parts[1], 320);
    if (key === undefined) return invalidName(response, method);
    if (method === "PUT") {
      if (!authorizePlatformOperation(this.client, target, capability, "put", true, response) || !requirePlatformRole(principal, "platform.admin", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, "put", true, { ...requireJSONObject(body), key }, response, signal));
      return true;
    }
    if (method === "DELETE") {
      if (!authorizePlatformOperation(this.client, target, capability, "delete", true, response) || !requirePlatformRole(principal, "platform.admin", response)) return true;
      const ifVersion = optionalNonnegativeVersion(request.url);
      if (ifVersion === "invalid") { sendAPIError(response, 400, "invalid_version"); return true; }
      await this.call(principal, target, "delete", true, { key, ...(ifVersion === undefined ? {} : { ifVersion }) }, response, signal, false, () => ({ deleted: true }));
      return true;
    }
    return methodNotAllowed(response, method);
  }

  private call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false, transform?: (value: unknown) => unknown): Promise<void> {
    return sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head, ...(transform === undefined ? {} : { transform }) });
  }
}

function listItems(value: unknown): unknown { if (typeof value !== "object" || value === null || !Array.isArray((value as Record<string, unknown>).items)) throw new Error("settings list 响应无效"); return (value as { items: unknown[] }).items; }
function methodNotAllowed(response: ServerResponse, method: string): true { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
function notFound(response: ServerResponse, method: string): true { sendAPIError(response, 404, "not_found", method === "HEAD"); return true; }
function invalidName(response: ServerResponse, method: string): true { sendAPIError(response, 400, "invalid_resource_name", method === "HEAD"); return true; }
