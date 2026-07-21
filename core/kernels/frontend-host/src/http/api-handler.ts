import type { IncomingMessage, ServerResponse } from "node:http";
import type { IdentityProvider } from "../identity/identity-provider";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { issueCSRF, validCSRF } from "../security/csrf";
import { sendAPIError, sendJSON } from "./json-response";
import { PortalControlRoutes } from "./portal-control-routes";

export interface APIHandlerOptions {
  identity: IdentityProvider;
  secureCookies: boolean;
  composer?: PortalComposerPort;
}

export function createAPIHandler(options: APIHandlerOptions): (request: IncomingMessage, response: ServerResponse, path: string) => Promise<void> {
  const portalControl = options.composer === undefined ? undefined : new PortalControlRoutes(options.composer);
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
    if (portalControl !== undefined) {
      const controller = new AbortController();
      request.once("aborted", () => controller.abort(new Error("Browser request aborted")));
      if (await portalControl.handle(path, method, principal, request, response, controller.signal)) return;
    }
    return sendAPIError(response, 404, "not_found", method === "HEAD");
  };
}
