import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { IdentityProvider, Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const capability = "foundation.security.seed.handoff";
const permissionPrefix = "foundation.security.seed";

export class PlatformSeedHandoffRoutes {
  public constructor(private readonly client: PlatformCapabilityPort, private readonly identity: IdentityProvider) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts[0] !== "seed-handoff") return false;
    const method = request.method ?? "GET";
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) return this.call(principal, target, "get", false, `${permissionPrefix}.state.read`, {}, response, signal, method === "HEAD");
    if (parts.length !== 2 || method !== "POST") return reject(response, 405, "method_not_allowed", method);
    const actions: Record<string, { operation: string; permission: string; proof: boolean }> = {
      "configure-provider": { operation: "configureProvider", permission: `${permissionPrefix}.provider.configure`, proof: false },
      "verify-provider": { operation: "verifyProvider", permission: `${permissionPrefix}.provider.configure`, proof: true },
      "prepare": { operation: "prepareHandoff", permission: `${permissionPrefix}.handoff.complete`, proof: true },
      "complete": { operation: "completeHandoff", permission: `${permissionPrefix}.handoff.complete`, proof: false },
    };
    const action = actions[parts[1] ?? ""];
    if (action === undefined) return reject(response, 404, "not_found", method);
    await withRequestJSON(request, response, async (body) => {
      const payload = { ...requireJSONObject(body) };
      if (action.proof) {
        const assertion = await this.identity.authenticationProof?.(request);
        if (assertion === undefined) { reject(response, 409, "fresh_enterprise_authentication_required", method); return; }
        payload.assertion = assertion;
      }
      await this.call(principal, target, action.operation, true, action.permission, payload, response, signal);
    });
    return true;
  }

  private async call(principal: Principal, target: PlatformManagementTarget, operation: string, write: boolean, permission: string, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false): Promise<true> {
    if (!authorizePlatformOperation(this.client, target, capability, operation, write, response) || !requirePlatformRole(principal, permission, response)) return true;
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write, payload, response, signal, head });
    return true;
  }
}

function reject(response: ServerResponse, status: number, code: string, method: string): true { sendAPIError(response, status, code, method === "HEAD"); return true; }
