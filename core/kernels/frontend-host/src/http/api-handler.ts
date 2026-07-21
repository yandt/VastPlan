import type { IncomingMessage, ServerResponse } from "node:http";
import type { IdentityProvider } from "../identity/identity-provider";
import { issueCSRF } from "../security/csrf";
import { sendAPIError, sendJSON } from "./json-response";

export interface APIHandlerOptions {
  identity: IdentityProvider;
  secureCookies: boolean;
}

export function createAPIHandler(options: APIHandlerOptions): (request: IncomingMessage, response: ServerResponse, path: string) => Promise<void> {
  return async (request, response, path) => {
    if (path !== "/v1/csrf") return sendAPIError(response, 404, "not_found", request.method === "HEAD");
    if (request.method !== "GET" && request.method !== "HEAD") return sendAPIError(response, 405, "method_not_allowed");
    try {
      const principal = await options.identity.authenticate(request);
      if (!principal.id || !principal.tenantId) return sendAPIError(response, 401, "session_required");
    } catch {
      return sendAPIError(response, 401, "session_required");
    }
    const token = issueCSRF(response, options.secureCookies);
    sendJSON(response, 200, { token }, request.method === "HEAD");
  };
}
