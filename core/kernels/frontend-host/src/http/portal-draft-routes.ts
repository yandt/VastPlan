import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { sendCapabilityResponse } from "./capability-response";
import { requireJSONObject, withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portal-drafts";

export class PortalDraftRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) return this.collection(method, principal, request, response, signal);
    const parts = path.slice(basePath.length + 1).split("/");
    if (parts.length < 1 || parts.length > 2 || !parts[0]) {
      sendAPIError(response, 404, "not_found", method === "HEAD");
      return true;
    }
    const revisionID = parseRevisionID(parts[0]);
    if (revisionID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    return this.revision(method, parts[1], revisionID, principal, request, response, signal);
  }

  private async collection(method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (method === "GET" || method === "HEAD") {
      await sendCapabilityResponse(this.composer, principal, "list", encodeCapabilityPayload({}), response, signal, method === "HEAD");
    } else if (method === "POST") {
      await withRequestJSON(request, response, async (body) => sendCapabilityResponse(this.composer, principal, "createDraft", encodeCapabilityPayload(body), response, signal));
    } else sendAPIError(response, 405, "method_not_allowed");
    return true;
  }

  private async revision(method: string, action: string | undefined, revisionID: number, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<true> {
    if (action === undefined && method === "PUT") {
      await withRequestJSON(request, response, async (composition) => sendCapabilityResponse(this.composer, principal, "updateDraft", encodeCapabilityPayload({ revisionId: revisionID, composition }), response, signal));
      return true;
    }
    if (action === "audit" && (method === "GET" || method === "HEAD")) {
      await sendCapabilityResponse(this.composer, principal, "audit", encodeCapabilityPayload({ revisionId: revisionID }), response, signal, method === "HEAD");
      return true;
    }
    if ((action === "submit" || action === "approve") && method === "POST") {
      await withRequestJSON(request, response, async () => sendCapabilityResponse(this.composer, principal, action, encodeCapabilityPayload({ revisionId: revisionID }), response, signal));
      return true;
    }
    if (action === "publish" && method === "POST") {
      await withRequestJSON(request, response, async (body) => sendCapabilityResponse(this.composer, principal, "publish", encodeCapabilityPayload({ ...requireJSONObject(body), revisionId: revisionID }), response, signal));
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }
}
