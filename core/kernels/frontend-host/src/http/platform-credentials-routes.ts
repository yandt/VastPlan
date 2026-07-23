import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { boundedQueryInteger, queryValue, requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.credentials";

export class PlatformCredentialsRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "credentials") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1) {
      if (!authorizePlatformOperation(this.client, target, capability, "list", false, response) || !requirePlatformRole(principal, "platform.credentials.read", response)) return true;
      if (method !== "GET" && method !== "HEAD") return reject(response, 405, "method_not_allowed", method);
      await this.call(principal, target, "list", false, { prefix: queryValue(request.url, "prefix") }, response, signal, method === "HEAD");
      return true;
    }
    if (parts.length === 2 && parts[1] === "managed-audit" && (method === "GET" || method === "HEAD")) {
      if (!authorizePlatformOperation(this.client, target, capability, "listManagedAudit", false, response) || !requirePlatformRole(principal, "platform.credentials.audit", response)) return true;
      const beforeId = boundedQueryInteger(request.url, "beforeId", 1, Number.MAX_SAFE_INTEGER);
      const parsedLimit = boundedQueryInteger(request.url, "limit", 1, 200);
      if (beforeId === "invalid" || parsedLimit === "invalid") return reject(response, 400, "invalid_query", method);
      const limit = parsedLimit ?? 100;
      await this.call(principal, target, "listManagedAudit", false, { ...(beforeId === undefined ? {} : { beforeId }), limit }, response, signal, method === "HEAD");
      return true;
    }
    const name = resourceName(parts[1], 160);
    if (name === undefined) return reject(response, 400, "invalid_resource_name", method);
    if (parts.length === 2 && method === "PUT") {
      if (!authorizePlatformOperation(this.client, target, capability, "put", true, response) || !requirePlatformRole(principal, "platform.credentials.write", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, "put", true, { ...requireJSONObject(body), name }, response, signal));
      return true;
    }
    const operation = parts.length === 3 && (parts[2] === "rotate" || parts[2] === "revoke") ? parts[2] : undefined;
    if (operation !== undefined && method === "POST") {
      if (!authorizePlatformOperation(this.client, target, capability, operation, true, response) || !requirePlatformRole(principal, `platform.credentials.${operation}`, response)) return true;
      await withRequestJSON(request, response, async () => this.call(principal, target, operation, true, { name }, response, signal));
      return true;
    }
    return reject(response, operation === undefined ? 404 : 405, operation === undefined ? "not_found" : "method_not_allowed", method);
  }

  private call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    return sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head });
  }
}

function reject(response: ServerResponse, status: number, code: string, method: string): true { sendAPIError(response, status, code, method === "HEAD"); return true; }
