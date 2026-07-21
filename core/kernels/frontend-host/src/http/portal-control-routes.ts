import type { ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { CapabilityApplicationError } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError, sendJSON } from "./json-response";

const emptyPayload = new TextEncoder().encode("{}");

export class PortalControlRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    const operation = path === "/v1/portal-drafts" ? "list" : path === "/v1/portal-governance" ? "governance" : undefined;
    if (operation === undefined) return false;
    if (method !== "GET" && method !== "HEAD") {
      sendAPIError(response, 405, "method_not_allowed");
      return true;
    }
    try {
      const raw = await this.composer.call(principal, operation, emptyPayload, signal);
      const value = JSON.parse(new TextDecoder().decode(raw)) as unknown;
      sendJSON(response, 200, value, method === "HEAD");
    } catch (error) {
      if (error instanceof CapabilityApplicationError && error.code === "permission.denied") sendAPIError(response, 403, "forbidden", method === "HEAD");
      else sendAPIError(response, 400, "request_rejected", method === "HEAD");
    }
    return true;
  }
}
