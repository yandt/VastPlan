import type { IncomingMessage, ServerResponse } from "node:http";
import type { IdentityProvider } from "../identity/identity-provider";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { InteractionPort } from "../capabilities/interaction-client";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import { issueCSRF, validCSRF } from "../security/csrf";
import { sendAPIError, sendJSON } from "./json-response";
import { PortalControlRoutes } from "./portal-control-routes";
import { InteractionRoutes } from "./interaction-routes";
import { PlatformManagementRoutes } from "./platform-management-routes";
import { PortalRuntimeRoutes } from "./portal-runtime-routes";

export interface APIHandlerOptions {
  identity: IdentityProvider;
  secureCookies: boolean;
  composer?: PortalComposerPort;
  interaction?: InteractionPort;
  platform?: { resolver: PlatformManagementResolver; client: PlatformCapabilityPort };
  delivery?: PortalDeliveryStore;
}

export function createAPIHandler(options: APIHandlerOptions): (request: IncomingMessage, response: ServerResponse, path: string) => Promise<void> {
  const portalControl = options.composer === undefined ? undefined : new PortalControlRoutes(options.composer);
  const interactions = options.interaction === undefined ? undefined : new InteractionRoutes(options.interaction);
  const platform = options.platform === undefined ? undefined : new PlatformManagementRoutes(options.platform.resolver, options.platform.client);
  const runtime = options.composer === undefined || options.delivery === undefined ? undefined : new PortalRuntimeRoutes(options.composer, options.delivery);
  return async (request, response, path) => {
    const method = request.method ?? "GET";
    let principal;
    try {
      principal = await options.identity.authenticate(request);
      if (!principal.id || !principal.tenantId) return sendAPIError(response, 401, "session_required");
    } catch {
      return sendAPIError(response, 401, "session_required");
    }
    if (path === "/v1/csrf") {
      if (method !== "GET" && method !== "HEAD") return sendAPIError(response, 405, "method_not_allowed");
      const token = issueCSRF(response, options.secureCookies);
      return sendJSON(response, 200, { token }, method === "HEAD");
    }
    if (method !== "GET" && method !== "HEAD" && !validCSRF(request)) return sendAPIError(response, 403, "csrf_rejected");
    const controller = new AbortController();
    request.once("aborted", () => controller.abort(new Error("Browser request aborted")));
    if (runtime !== undefined) {
      if (await runtime.handle(path, principal, request, response, controller.signal)) return;
    }
    if (portalControl !== undefined) {
      if (await portalControl.handle(path, method, principal, request, response, controller.signal)) return;
    }
    if (interactions !== undefined) {
      if (await interactions.handle(path, method, principal, request, response, controller.signal)) return;
    }
    if (platform !== undefined) {
      if (await platform.handle(path, principal, request, response, controller.signal)) return;
    }
    return sendAPIError(response, 404, "not_found", method === "HEAD");
  };
}
