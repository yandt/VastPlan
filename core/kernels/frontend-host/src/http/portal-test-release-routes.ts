import type { IncomingMessage, ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendComposerResponse } from "./composer-response";
import { sendAPIError } from "./json-response";
import { withRequestJSON } from "./request-json";
import { encodeCapabilityPayload, parseRevisionID } from "./revision-route-contract";

const basePath = "/v1/portal-governance/test-releases";

export class PortalTestReleaseRoutes {
  public constructor(private readonly composer: PortalComposerPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "GET" || method === "HEAD") await this.call("listTestReleases", {}, principal, response, signal, method === "HEAD");
      else if (method === "POST") await withRequestJSON(request, response, async (release) => this.call("createTestRelease", release, principal, response, signal));
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    const releaseID = parseRevisionID(parts[0]);
    if (releaseID === undefined) {
      sendAPIError(response, 400, "invalid_revision", method === "HEAD");
      return true;
    }
    if (parts.length === 2 && parts[1] === "rollback" && method === "POST") {
      await withRequestJSON(request, response, async () => this.call("rollbackTestRelease", { id: releaseID }, principal, response, signal));
    } else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    await sendComposerResponse(this.composer, principal, operation, encodeCapabilityPayload(payload), response, signal, head);
  }
}
