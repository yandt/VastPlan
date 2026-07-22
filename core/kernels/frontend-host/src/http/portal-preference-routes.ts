import type { IncomingMessage, ServerResponse } from "node:http";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { PortalPreferencePort } from "../capabilities/portal-preference-client";
import type { Principal } from "../identity/identity-provider";
import { PortalActivationCatalog, type PortalActivation } from "../runtime/portal-activation-catalog";
import { parsePortalPreference, parsePreferencePutBody, preferenceScopeForPortal, type PortalPreference, type PortalPreferenceScope } from "../runtime/portal-preference-contract";
import { sendAPIError, sendJSON } from "./json-response";
import { requestedPortalPath } from "./portal-runtime-path";
import { requestHostname } from "./platform-route-contract";
import { readRequestJSON, RequestJSONError } from "./request-json";

const endpoint = "/v1/portal-preference";
const encoder = new TextEncoder();
const decoder = new TextDecoder();

export class PortalPreferenceRoutes {
  private readonly activations: PortalActivationCatalog;

  public constructor(composer: PortalComposerPort, private readonly preferences: PortalPreferencePort) {
    this.activations = new PortalActivationCatalog(composer);
  }

  public async handle(path: string, principal: Principal, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (path !== endpoint) return false;
    const method = request.method ?? "GET";
    if (method !== "GET" && method !== "HEAD" && method !== "PUT") {
      response.setHeader("Allow", "GET, HEAD, PUT");
      sendAPIError(response, 405, "method_not_allowed", method === "HEAD");
      return true;
    }
    const requested = requestedPortalPath(request.url);
    if (requested === undefined) {
      sendAPIError(response, 400, "invalid_portal_path", method === "HEAD");
      return true;
    }
    let active: PortalActivation | undefined;
    try {
      const activations = await this.activations.list(principal, signal);
      active = this.activations.selectCurrent(activations, principal, requested, requestHostname(request));
    } catch {
      sendAPIError(response, 502, "portal_preference_unavailable", method === "HEAD");
      return true;
    }
    if (active === undefined) {
      sendAPIError(response, 404, "portal_not_found", method === "HEAD");
      return true;
    }
    if (!this.activations.audienceAllows(active, principal)) {
      sendAPIError(response, 403, "portal_audience_forbidden", method === "HEAD");
      return true;
    }
    let scope: PortalPreferenceScope;
    try { scope = preferenceScopeForPortal(active.resolved); }
    catch { sendAPIError(response, 502, "portal_preference_unavailable", method === "HEAD"); return true; }
    let operation: "get" | "put" = "get";
    let payload: unknown = { scope };
    if (method === "PUT") {
      try {
        operation = "put";
        const body = parsePreferencePutBody(await readRequestJSON(request, 256 << 10));
        payload = { scope, expectedRevision: body.expectedRevision, values: body.values };
      } catch (error) {
        if (error instanceof RequestJSONError || error instanceof Error) {
          sendAPIError(response, 400, "portal_preference_invalid");
          return true;
        }
        throw error;
      }
    }
    let raw: Uint8Array;
    try { raw = await this.preferences.call(principal, operation, encoder.encode(JSON.stringify(payload)), signal); }
    catch (error) {
      if (error instanceof CapabilityApplicationError) {
        if (error.code === "portal.preference.conflict") sendAPIError(response, 409, "portal_preference_conflict", method === "HEAD");
        else if (error.code === "portal.preference.invalid") sendAPIError(response, 400, "portal_preference_invalid", method === "HEAD");
        else if (error.code === "permission.denied") sendAPIError(response, 403, "portal_preference_forbidden", method === "HEAD");
        else sendAPIError(response, 502, "portal_preference_unavailable", method === "HEAD");
      } else sendAPIError(response, 502, "portal_preference_unavailable", method === "HEAD");
      return true;
    }
    let preference: PortalPreference;
    try { preference = parsePortalPreference(JSON.parse(decoder.decode(raw)) as unknown, scope); }
    catch { sendAPIError(response, 502, "portal_preference_unavailable", method === "HEAD"); return true; }
    response.setHeader("Cache-Control", "private, no-store");
    response.setHeader("Vary", "Cookie");
    sendJSON(response, 200, preference, method === "HEAD");
    return true;
  }
}
