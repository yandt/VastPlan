import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.database";

export class PlatformDatabaseRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "database-connections") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1) {
      if (!authorizePlatformOperation(this.client, target, capability, "list", false, response) || !requirePlatformRole(principal, "platform.database.read", response)) return true;
      if (method !== "GET" && method !== "HEAD") return reject(response, 405, "method_not_allowed", method);
      await this.call(principal, target, "list", false, {}, response, signal, method === "HEAD");
      return true;
    }
    const name = resourceName(parts[1], 160);
    if (name === undefined) return reject(response, 400, "invalid_resource_name", method);
    if (parts.length === 2 && (method === "PUT" || method === "DELETE")) {
      const operation = method === "PUT" ? "define" : "remove";
      if (!authorizePlatformOperation(this.client, target, capability, operation, true, response) || !requirePlatformRole(principal, "platform.database.write", response)) return true;
      if (method === "PUT") await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, { ...requireJSONObject(body), name }, response, signal));
      else await this.call(principal, target, operation, true, { name }, response, signal, false, () => ({ deleted: true }));
      return true;
    }
    if (parts.length === 3 && parts[2] === "probe" && method === "POST") {
      if (!authorizePlatformOperation(this.client, target, capability, "probe", true, response) || !requirePlatformRole(principal, "platform.database.probe", response)) return true;
      await withRequestJSON(request, response, async () => this.call(principal, target, "probe", true, { name }, response, signal));
      return true;
    }
    return reject(response, 405, "method_not_allowed", method);
  }

  private call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false, transform?: (value: unknown) => unknown): Promise<void> {
    return sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head, ...(transform === undefined ? {} : { transform }) });
  }
}

function reject(response: ServerResponse, status: number, code: string, method: string): true { sendAPIError(response, status, code, method === "HEAD"); return true; }
