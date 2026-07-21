import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";
import { requireEmptyJSONObject, withRequestJSON } from "./request-json";

const capability = "platform.artifacts.repository";
const mutations: Readonly<Record<string, string>> = Object.freeze({ quarantine: "gcQuarantine", sweep: "gcSweep" });

export class PlatformArtifactGCRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const operation = parts.length === 2 && parts[0] === "gc" ? mutations[parts[1]!] : undefined;
    if (operation === undefined) return false;
    if (!authorizePlatformOperation(this.client, target, capability, operation, true, response) || !requirePlatformRole(principal, "platform.artifacts.gc", response)) return true;
    if (request.method !== "POST") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    await withRequestJSON(request, response, async (payload) => sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: true, payload: operation === "gcSweep" ? requireEmptyJSONObject(payload) : payload, response, signal }));
    return true;
  }
}
