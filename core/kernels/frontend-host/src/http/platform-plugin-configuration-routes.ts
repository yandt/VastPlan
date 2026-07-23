import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, responseItems, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.plugin-configuration";

export class PlatformPluginConfigurationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "plugin-configurations") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1) {
      if (method !== "GET" && method !== "HEAD") return methodNotAllowed(response, method);
      if (!this.authorize(principal, target, "listDefinitions", false, "platform.plugin-configuration.read", response)) return true;
      await this.call(principal, target, "listDefinitions", false, {}, response, signal, method === "HEAD", responseItems);
      return true;
    }
    if (parts[1] === "candidates") return this.handleCandidates(parts, principal, target, request, response, signal);
    if (parts.length !== 2 || (method !== "GET" && method !== "HEAD")) return parts.length === 2 ? methodNotAllowed(response, method) : notFound(response, method);
    const configurationId = resourceName(parts[1], 64);
    if (configurationId === undefined) return invalidName(response, method);
    if (!this.authorize(principal, target, "getDefinition", false, "platform.plugin-configuration.read", response)) return true;
    const catalogDigest = new URL(request.url ?? "/", "https://portal.invalid").searchParams.get("catalogDigest") ?? "";
    await this.call(principal, target, "getDefinition", false, { configurationId, ...(catalogDigest === "" ? {} : { catalogDigest }) }, response, signal, method === "HEAD");
    return true;
  }

  private async handleCandidates(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    const method = request.method ?? "GET";
    if (parts.length === 2 && (method === "GET" || method === "HEAD")) {
      if (!this.authorize(principal, target, "listCandidates", false, "platform.plugin-configuration.read", response)) return true;
      await this.call(principal, target, "listCandidates", false, {}, response, signal, method === "HEAD", responseItems);
      return true;
    }
    if (parts.length === 2 && method === "POST") {
      if (!this.authorize(principal, target, "createDraft", true, "platform.plugin-configuration.write", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, "createDraft", true, requireJSONObject(body), response, signal));
      return true;
    }
    if (parts.length === 3 && method === "DELETE") {
      const id = resourceName(parts[2], 64);
      if (id === undefined) return invalidName(response, method);
      if (!this.authorize(principal, target, "discardDraft", true, "platform.plugin-configuration.write", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, "discardDraft", true, { ...requireJSONObject(body), id }, response, signal));
      return true;
    }
    if (parts.length === 4 && method === "POST" && (parts[3] === "submit" || parts[3] === "activate")) {
      const id = resourceName(parts[2], 64);
      if (id === undefined) return invalidName(response, method);
      const operation = parts[3] === "submit" ? "submitDraft" : "activateCandidate";
      if (!this.authorize(principal, target, operation, true, "platform.plugin-configuration.publish", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, { ...requireJSONObject(body), id }, response, signal));
      return true;
    }
    return parts.length > 4 ? notFound(response, method) : methodNotAllowed(response, method);
  }

  private authorize(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, role: string, response: ServerResponse): boolean {
    return authorizePlatformOperation(this.client, target, capability, operation, write, response) && requirePlatformRole(principal, role, response);
  }

  private call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false, transform?: (value: unknown) => unknown): Promise<void> {
    return sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head, ...(transform === undefined ? {} : { transform }) });
  }
}

function methodNotAllowed(response: ServerResponse, method: string): true { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
function notFound(response: ServerResponse, method: string): true { sendAPIError(response, 404, "not_found", method === "HEAD"); return true; }
function invalidName(response: ServerResponse, method: string): true { sendAPIError(response, 400, "invalid_resource_name", method === "HEAD"); return true; }
