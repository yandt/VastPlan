import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole, resourceName } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.artifacts.repository";
const refValue = /^[a-z][a-z0-9]*(?:[._-][a-z0-9]+)+$/;
const versionValue = /^[0-9A-Za-z][0-9A-Za-z.+-]{0,127}$/;
const channelValue = /^[a-z0-9][a-z0-9._-]{0,63}$/;

export class PlatformArtifactPublicationRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const method = request.method ?? "GET";
    if (parts.length === 1 && parts[0] === "publications" && (method === "GET" || method === "HEAD")) {
      return this.read("listPublications", {}, principal, target, response, signal, method === "HEAD");
    }
    if (parts.length === 1 && parts[0] === "evidence" && (method === "GET" || method === "HEAD")) {
      const payload = evidenceQuery(request.url);
      if (payload === undefined) { sendAPIError(response, 400, "invalid_artifact_ref", method === "HEAD"); return true; }
      return this.read("getSupplyChainEvidence", payload, principal, target, response, signal, method === "HEAD");
    }
    if (parts.length === 1 && parts[0] === "publications") {
      return this.write("submitPublication", "platform.artifacts.publication.submit", undefined, principal, target, request, response, signal);
    }
    if (parts.length === 3 && parts[0] === "publications" && parts[2] === "approve") {
      const id = resourceName(parts[1], 64);
      if (id === undefined || !/^[a-f0-9]{64}$/.test(id)) { sendAPIError(response, 400, "invalid_publication_id"); return true; }
      return this.write("approvePublication", "platform.artifacts.publication.approve", id, principal, target, request, response, signal);
    }
    return false;
  }

  private async read(operation: string, payload: Record<string, unknown>, principal: Principal, target: PlatformManagementTarget, response: ServerResponse, signal: AbortSignal, head: boolean): Promise<true> {
    if (!authorizePlatformOperation(this.client, target, capability, operation, false, response) || !requirePlatformRole(principal, "platform.artifacts.read", response)) return true;
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: false, payload, response, signal, head });
    return true;
  }

  private async write(operation: string, permission: string, id: string | undefined, principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (!authorizePlatformOperation(this.client, target, capability, operation, true, response) || !requirePlatformRole(principal, permission, response)) return true;
    if (request.method !== "POST") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    await withRequestJSON(request, response, async (body) => {
      const payload = requireJSONObject(body);
      await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: true, payload: id === undefined ? payload : { ...payload, id }, response, signal });
    });
    return true;
  }
}

function evidenceQuery(url: string | undefined): Record<string, unknown> | undefined {
  const params = new URL(url ?? "/", "https://portal.invalid").searchParams;
  const keys = [...params.keys()];
  if (keys.length !== 3 || new Set(keys).size !== 3 || !["pluginId", "version", "channel"].every((key) => params.has(key))) return undefined;
  const pluginId = params.get("pluginId") ?? "", version = params.get("version") ?? "", channel = params.get("channel") ?? "";
  return refValue.test(pluginId) && versionValue.test(version) && channelValue.test(channel) ? { pluginId, version, channel } : undefined;
}
