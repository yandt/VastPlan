import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { withRequestJSON } from "./request-json";
import { encodeCapabilityPayload } from "./revision-route-contract";

const basePath = "/v1/portal-governance/test-target-bindings";
const resourceID = /^[a-z0-9][a-z0-9._-]{0,127}$/;

export class PortalTestTargetRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "GET" || method === "HEAD") await this.call("listTestTargetBindings", {}, principal, response, signal, method === "HEAD");
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    if (parts.length === 1 && resourceID.test(parts[0]!) && method === "PUT") {
      await withRequestJSON(request, response, async (binding) => this.call("putTestTargetBinding", { id: parts[0], binding }, principal, response, signal));
    } else sendAPIError(response, 404, "not_found", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    await sendCapabilityResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal, head);
  }
}
