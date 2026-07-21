import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { sendComposerResponse } from "./composer-response";
import { PortalDraftRoutes } from "./portal-draft-routes";

const emptyPayload = new TextEncoder().encode("{}");

export class PortalControlRoutes {
  private readonly drafts: PortalDraftRoutes;

  public constructor(private readonly composer: PortalComposerPort) {
    this.drafts = new PortalDraftRoutes(composer);
  }

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (await this.drafts.handle(path, method, principal, request, response, signal)) return true;
    if (path !== "/v1/portal-governance") return false;
    if (method !== "GET" && method !== "HEAD") {
      sendAPIError(response, 405, "method_not_allowed");
      return true;
    }
    await sendComposerResponse(this.composer, principal, "governance", emptyPayload, response, signal, method === "HEAD");
    return true;
  }
}
