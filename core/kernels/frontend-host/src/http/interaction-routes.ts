import type { IncomingMessage, ServerResponse } from "node:http";
import type { InteractionPort } from "../capabilities/interaction-client";
import type { Principal } from "../identity/identity-provider";
import { sendCapabilityResponse } from "./capability-response";
import { sendAPIError } from "./json-response";
import { withRequestJSON } from "./request-json";
import { encodeCapabilityPayload } from "./revision-route-contract";

const basePath = "/v1/interactions";

export class InteractionRoutes {
  public constructor(private readonly interaction: InteractionPort) {}

  public async handle(path: string, method: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== basePath && !path.startsWith(`${basePath}/`)) return false;
    if (path === basePath) {
      if (method === "GET" || method === "HEAD") await this.call("list", { surface: "frontend" }, principal, response, signal, method === "HEAD");
      else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const parts = path.slice(basePath.length + 1).split("/");
    const id = interactionID(parts[0]);
    if (id === undefined || parts.length > 2) {
      sendAPIError(response, 404, "not_found", method === "HEAD");
      return true;
    }
    if (parts.length === 1 && (method === "GET" || method === "HEAD")) {
      await this.call("get", { id }, principal, response, signal, method === "HEAD");
    } else if (parts[1] === "present" && method === "POST") {
      await withRequestJSON(request, response, async () => this.call("present", { id, surface: "frontend" }, principal, response, signal));
    } else if (parts[1] === "respond" && method === "POST") {
      await withRequestJSON(request, response, async (value) => this.call("respond", { id, surface: "frontend", response: value }, principal, response, signal));
    } else sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  private async call(operation: string, payload: unknown, principal: Principal, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    await sendCapabilityResponse(this.interaction, principal, operation, encodeCapabilityPayload(payload), response, signal, head);
  }
}

function interactionID(value: string | undefined): string | undefined {
  if (value === undefined || value.length === 0 || value.length > 256 || value.includes("\\") || /[\u0000-\u001f\u007f]/.test(value)) return undefined;
  try {
    const decoded = decodeURIComponent(value);
    return decoded.includes("/") || decoded.length === 0 ? undefined : decoded;
  } catch { return undefined; }
}
