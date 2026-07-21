import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, lifecycleActions, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portal-governance/bindings";

export class PortalBindingRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "POST") await withRequestJSON(request, response, async (draft) => this.call("createBindingDraft", draft, principal, response, signal));
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    const revisionID = parseRevisionID(parts[0]);
    if (revisionID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    if (parts.length === 1 && method === "PUT") {
      await withRequestJSON(request, response, async (draft) => this.call("updateBindingDraft", { revisionId: revisionID, draft }, principal, response, signal));
    } else if (parts.length === 2 && lifecycleActions.has(parts[1]!) && method === "POST") {
      await withRequestJSON(request, response, async () => this.call("transitionBinding", { revisionId: revisionID, action: parts[1] }, principal, response, signal));
    } else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal): Promise<void> {
    await sendCapabilityResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal);
  }
}
