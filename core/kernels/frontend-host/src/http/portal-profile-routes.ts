import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseLifecycleAction, parseRevisionID, profileLifecycleOperations } from "./revision-route-contract";

const basePath = "/v1/portal-governance/profiles";

export class PortalProfileRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "POST") await withRequestJSON(request, response, async (profile) => this.call("createProfileDraft", profile, principal, response, signal));
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    const revisionID = parseRevisionID(parts[0]);
    if (revisionID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    const lifecycleAction = parts.length === 2 ? parseLifecycleAction(parts[1]) : undefined;
    if (parts.length === 1 && method === "PUT") {
      await withRequestJSON(request, response, async (profile) => this.call("updateProfileDraft", { revisionId: revisionID, profile }, principal, response, signal));
    } else if (parts.length === 1 && method === "DELETE") {
      await this.call("deleteProfileDraft", { revisionId: revisionID }, principal, response, signal);
    } else if (lifecycleAction !== undefined && method === "POST") {
      await withRequestJSON(request, response, async () => this.call(profileLifecycleOperations[lifecycleAction], { revisionId: revisionID }, principal, response, signal));
    } else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: Parameters<PortalComposerPort["call"]>[1], payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal): Promise<void> {
    await sendCapabilityResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal);
  }
}
