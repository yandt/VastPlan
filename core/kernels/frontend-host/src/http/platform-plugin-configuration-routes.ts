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
    if (parts[1] === "resources") return this.handleResources(parts, principal, target, request, response, signal);
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
    const profileOperations: Readonly<Record<string, string>> = {
      "submit-profile": "submitProfileDraft",
      "approve-profile": "approveProfileCandidate",
      "activate-profile": "activateProfileCandidate",
      "abort-profile": "abortProfileCandidate",
    };
    if (parts.length === 4 && method === "POST" && parts[3] !== undefined && profileOperations[parts[3]] !== undefined) {
      const id = resourceName(parts[2], 64);
      if (id === undefined) return invalidName(response, method);
      const operation = profileOperations[parts[3]]!;
      if (!this.authorize(principal, target, operation, true, "platform.plugin-configuration.profile.publish", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, { ...requireJSONObject(body), id }, response, signal));
      return true;
    }
    const hotOperations: Readonly<Record<string, string>> = {
      "submit-hot": "submitHotServiceDraft",
      "approve-hot": "approveHotServiceCandidate",
      "activate-hot": "activateHotServiceCandidate",
      "abort-hot": "abortHotServiceCandidate",
    };
    if (parts.length === 4 && method === "POST" && parts[3] !== undefined && hotOperations[parts[3]] !== undefined) {
      const id = resourceName(parts[2], 64);
      if (id === undefined) return invalidName(response, method);
      const operation = hotOperations[parts[3]]!;
      if (!this.authorize(principal, target, operation, true, "platform.plugin-configuration.hot.publish", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, { ...requireJSONObject(body), id }, response, signal));
      return true;
    }
    const resourceOperations: Readonly<Record<string, string>> = {
      "submit-resource": "submitResourceDraft",
      "approve-resource": "approveResourceCandidate",
      "activate-resource": "activateResourceCandidate",
      "abort-resource": "abortResourceCandidate",
    };
    if (parts.length === 4 && method === "POST" && parts[3] !== undefined && resourceOperations[parts[3]] !== undefined) {
      const id = resourceName(parts[2], 64);
      if (id === undefined) return invalidName(response, method);
      const operation = resourceOperations[parts[3]]!;
      if (!this.authorize(principal, target, operation, true, "platform.plugin-configuration.resource.publish", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, { ...requireJSONObject(body), id }, response, signal));
      return true;
    }
    return parts.length > 4 ? notFound(response, method) : methodNotAllowed(response, method);
  }

  private async handleResources(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    const method = request.method ?? "GET";
    if (parts.length === 2 && (method === "GET" || method === "HEAD")) {
      if (!this.authorize(principal, target, "listResourceItems", false, "platform.plugin-configuration.read", response)) return true;
      const query = resourceQuery(request);
      if (query === undefined) return invalidName(response, method);
      await this.call(principal, target, "listResourceItems", false, query, response, signal, method === "HEAD");
      return true;
    }
    if (parts.length === 3 && parts[2] !== "candidates" && (method === "GET" || method === "HEAD")) {
      if (!this.authorize(principal, target, "getResourceItem", false, "platform.plugin-configuration.read", response)) return true;
      const resourceId = resourceName(parts[2], 64), query = resourceQuery(request);
      if (resourceId === undefined || query === undefined) return invalidName(response, method);
      await this.call(principal, target, "getResourceItem", false, { ...query, resourceId }, response, signal, method === "HEAD");
      return true;
    }
    const draftOperations: Readonly<Record<string, string>> = { create: "createResourceDraft", update: "updateResourceDraft", delete: "deleteResourceDraft" };
    if (parts.length === 4 && parts[2] === "candidates" && method === "POST" && parts[3] !== undefined && draftOperations[parts[3]] !== undefined) {
      const operation = draftOperations[parts[3]]!;
      if (!this.authorize(principal, target, operation, true, "platform.plugin-configuration.write", response)) return true;
      await withRequestJSON(request, response, async (body) => this.call(principal, target, operation, true, requireJSONObject(body), response, signal));
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

function resourceQuery(request: IncomingMessage): Record<string, unknown> | undefined {
  const query = new URL(request.url ?? "/", "https://portal.invalid").searchParams;
  const configurationId = resourceName(query.get("configurationId") ?? "", 64);
  const resourceCollectionId = resourceName(query.get("resourceCollectionId") ?? "", 64);
  const catalogDigest = resourceName(query.get("catalogDigest") ?? "", 128);
  if (configurationId === undefined || resourceCollectionId === undefined || catalogDigest === undefined) return undefined;
  const cursor = query.get("cursor") ?? "", rawLimit = query.get("limit") ?? "";
  const limit = rawLimit === "" ? undefined : Number(rawLimit);
  if (cursor.length > 512 || (limit !== undefined && (!Number.isSafeInteger(limit) || limit < 1 || limit > 256))) return undefined;
  return { configurationId, resourceCollectionId, catalogDigest, ...(cursor === "" ? {} : { cursor }), ...(limit === undefined ? {} : { limit }) };
}

function methodNotAllowed(response: ServerResponse, method: string): true { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
function notFound(response: ServerResponse, method: string): true { sendAPIError(response, 404, "not_found", method === "HEAD"); return true; }
function invalidName(response: ServerResponse, method: string): true { sendAPIError(response, 400, "invalid_resource_name", method === "HEAD"); return true; }
