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

type PreferenceInvocation = { operation: "get" | "put"; payload: unknown };

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
    let invocation: PreferenceInvocation;
    try {
      invocation = await preferenceInvocation(method, request, scope);
    } catch (error) {
      if (!(error instanceof RequestJSONError) && !(error instanceof Error)) throw error;
      sendAPIError(response, 400, "portal_preference_invalid");
      return true;
    }
    let raw: Uint8Array;
    try { raw = await this.preferences.call(principal, invocation.operation, encoder.encode(JSON.stringify(invocation.payload)), signal); }
    catch (error) {
      sendPreferenceCallError(response, error, method === "HEAD");
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

async function preferenceInvocation(method: string, request: IncomingMessage, scope: PortalPreferenceScope): Promise<PreferenceInvocation> {
  if (method !== "PUT") return { operation: "get", payload: { scope } };
  const body = parsePreferencePutBody(await readRequestJSON(request, 256 << 10));
  return { operation: "put", payload: { scope, expectedRevision: body.expectedRevision, values: body.values } };
}

function sendPreferenceCallError(response: ServerResponse, error: unknown, head: boolean): void {
  const failures: Record<string, { status: number; code: string }> = {
    "portal.preference.conflict": { status: 409, code: "portal_preference_conflict" },
    "portal.preference.invalid": { status: 400, code: "portal_preference_invalid" },
    "permission.denied": { status: 403, code: "portal_preference_forbidden" },
  };
  const failure = error instanceof CapabilityApplicationError ? failures[error.code] : undefined;
  sendAPIError(response, failure?.status ?? 502, failure?.code ?? "portal_preference_unavailable", head);
}
