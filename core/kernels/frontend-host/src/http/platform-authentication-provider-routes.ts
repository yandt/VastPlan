import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "foundation.security.authentication.providers";
const permissionPrefix = "foundation.security.authentication.providers";

export class PlatformAuthenticationProviderRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "authentication-providers") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) return this.call(principal, target, "get", false, `${permissionPrefix}.read`, {}, response, signal, method === "HEAD");
    if (parts.length === 1 && method === "POST") { await withRequestJSON(request, response, async (body) => { await this.call(principal, target, "createDraft", true, `${permissionPrefix}.edit`, requireJSONObject(body), response, signal); }); return true; }
    if (parts.length === 2 && parts[1] === "publish" && method === "POST") { await withRequestJSON(request, response, async (body) => { await this.call(principal, target, "publish", true, `${permissionPrefix}.publish`, requireJSONObject(body), response, signal); }); return true; }
    const providerId = resourceName(parts[1], 160);
    if (providerId === undefined || parts.length !== 3 || method !== "POST") return reject(response, 405, "method_not_allowed", method);
    const actions: Record<string, { operation: string; permission: string }> = {
      validate: { operation: "validate", permission: `${permissionPrefix}.edit` },
      test: { operation: "recordTest", permission: `${permissionPrefix}.test` },
      approve: { operation: "approve", permission: `${permissionPrefix}.approve` },
      retire: { operation: "retire", permission: `${permissionPrefix}.edit` },
    };
    const action = actions[parts[2] ?? ""];
    if (action === undefined) return reject(response, 404, "not_found", method);
    await withRequestJSON(request, response, async (body) => { await this.call(principal, target, action.operation, true, action.permission, { ...requireJSONObject(body), providerId }, response, signal); });
    return true;
  }

  private async call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, permission: string, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false): Promise<true> {
    if (!authorizePlatformOperation(this.client, target, capability, operation, write, response) || !requirePlatformRole(principal, permission, response)) return true;
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head });
    return true;
  }
}

function reject(response: ServerResponse, status: number, code: string, method: string): true { sendAPIError(response, status, code, method === "HEAD"); return true; }
