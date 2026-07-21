import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendComposerResponse } from "./composer-response";
import { sendAPIError } from "./json-response";
import { requireJSONObject, withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portal-governance/activations";

export class PortalActivationRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "POST") await withRequestJSON(request, response, async (activation) => this.call("activate", activation, principal, response, signal));
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    const sourceID = parseRevisionID(parts[0]);
    if (sourceID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    if (parts.length === 2 && parts[1] === "rollback" && method === "POST") {
      await withRequestJSON(request, response, async (body) => this.call("rollbackActivation", { ...requireJSONObject(body), sourceId: sourceID }, principal, response, signal));
    } else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal): Promise<void> {
    await sendComposerResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal);
  }
}
