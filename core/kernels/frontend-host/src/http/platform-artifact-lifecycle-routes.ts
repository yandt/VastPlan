import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";
import { withRequestJSON } from "./request-json";

const capability = "platform.artifacts.repository";

export class PlatformArtifactLifecycleRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts.length !== 1 || parts[0] !== "lifecycle") return false;
    if (!authorizePlatformOperation(this.client, target, capability, "setLifecycle", true, response) || !requirePlatformRole(principal, "platform.artifacts.lifecycle", response)) return true;
    if (request.method !== "POST") { sendAPIError(response, 405, "method_not_allowed"); return true; }
    await withRequestJSON(request, response, async (payload) => sendPlatformResponse({ client: this.client, principal, target, capability, operation: "setLifecycle", write: true, payload, response, signal }));
    return true;
  }
}
