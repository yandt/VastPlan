import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformControlBootstrapPort } from "../capabilities/platform-control-bootstrap-client";
import type { Principal } from "../identity/identity-provider";
import { reportCapabilityFailure } from "../observability/capability-failure";
import { sendAPIError, sendJSON } from "./json-response";
import { requirePlatformRole } from "./platform-route-contract";
import { sendCapabilityFailure } from "./platform-response";
import { encodeCapabilityPayload } from "./revision-route-contract";
import { requireJSONObject, withRequestJSON } from "./request-json";

const path = "/v1/bootstrap/platform-control";
type Operation = "platformControlStatus" | "platformControlTest" | "platformControlConfigure";

export class BootstrapPlatformControlRoutes {
  public constructor(private readonly client: PlatformControlBootstrapPort) {}

  public async handle(requestPath: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (requestPath !== path && requestPath !== `${path}/test`) return false;
    const method = request.method ?? "GET";
    if (requestPath === path && (method === "GET" || method === "HEAD")) {
      if (!requirePlatformRole(principal, "platform.database.read", response)) return true;
      await this.forward(principal, "platformControlStatus", {}, response, signal, method === "HEAD");
      return true;
    }
    if (requestPath === `${path}/test` && method === "POST") {
      if (!requirePlatformRole(principal, "platform.database.probe", response)) return true;
      await withRequestJSON(request, response, async (body) => this.forwardCandidate(principal, "platformControlTest", requireJSONObject(body), response, signal));
      return true;
    }
    if (requestPath === path && method === "PUT") {
      if (!requirePlatformRole(principal, "platform.database.write", response)) return true;
      await withRequestJSON(request, response, async (body) => this.forwardCandidate(principal, "platformControlConfigure", requireJSONObject(body), response, signal));
      return true;
    }
    sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
    return true;
  }

  public async status(principal: Principal, signal?: AbortSignal): Promise<Readonly<Record<string, unknown>> | undefined> {
    try {
      const raw = await this.client.call(principal, "platformControlStatus", encodeCapabilityPayload({}), signal);
      const value = JSON.parse(new TextDecoder().decode(raw)) as unknown;
      return typeof value === "object" && value !== null && !Array.isArray(value) ? value as Readonly<Record<string, unknown>> : undefined;
    } catch {
      return undefined;
    }
  }

  private async forwardCandidate(principal: Principal, operation: Exclude<Operation, "platformControlStatus">, payload: unknown, response: ServerResponse, signal: AbortSignal): Promise<void> {
    const current = await this.status(principal, signal);
    if (current?.phase === "ready") {
      sendAPIError(response, 409, "platform_control_ready");
      return;
    }
    await this.forward(principal, operation, payload, response, signal);
  }

  private async forward(principal: Principal, operation: Operation, payload: unknown, response: ServerResponse, signal: AbortSignal, head = false): Promise<void> {
    try {
      const raw = await this.client.call(principal, operation, encodeCapabilityPayload(payload), signal);
      let value: unknown;
      try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
      catch { return sendAPIError(response, 502, "platform_service_unavailable", head); }
      sendJSON(response, 200, value, head);
    } catch (error) {
      reportCapabilityFailure({ operation, capability: "platform.database", logicalService: this.client.logicalService }, error);
      sendCapabilityFailure(response, error, head);
    }
  }
}
