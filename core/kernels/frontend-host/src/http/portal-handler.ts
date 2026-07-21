import type { IncomingMessage, ServerResponse } from "node:http";
import { PortalAssets } from "../assets/portal-assets";
import type { IdentityProvider } from "../identity/identity-provider";
import { createAPIHandler } from "./api-handler";
import { setBaseSecurityHeaders, setIndexSecurityHeaders } from "./security-headers";

export interface PortalHandlerOptions {
  assets: PortalAssets;
  identity?: IdentityProvider;
  secureCookies?: boolean;
}

export function createPortalHandler(options: PortalHandlerOptions): (request: IncomingMessage, response: ServerResponse) => void {
  const api = options.identity === undefined ? undefined : createAPIHandler({ identity: options.identity, secureCookies: options.secureCookies ?? true });
  return (request, response) => {
    setBaseSecurityHeaders(response);
    const method = request.method ?? "GET";
    const path = requestPath(request.url);
    if (path === undefined) return sendEmpty(response, 400);
    if (path === "/v1" || path.startsWith("/v1/")) {
      if (api === undefined) return sendEmpty(response, 404);
      void api(request, response, path);
      return;
    }
    if (method !== "GET" && method !== "HEAD") return sendEmpty(response, 405, { Allow: "GET, HEAD" });
    if (path === "/healthz" || path === "/readyz") return sendText(response, method, 200, "ok\n");
    if (path.startsWith("/assets/")) return serveAsset(options.assets, path.slice("/assets/".length), method, request, response);
    const index = options.assets.renderIndex();
    setIndexSecurityHeaders(response, index.nonce);
    response.statusCode = 200;
    if (method === "GET") response.end(index.body);
    else response.end();
  };
}

function requestPath(value: string | undefined): string | undefined {
  try {
    return new URL(value ?? "/", "https://portal.invalid").pathname;
  } catch {
    return undefined;
  }
}

function serveAsset(assets: PortalAssets, name: string, method: string, request: IncomingMessage, response: ServerResponse): void {
  if (!validAssetName(name)) return sendEmpty(response, 404);
  const asset = assets.get(name);
  if (asset === undefined) return sendEmpty(response, 404);
  response.setHeader("ETag", asset.etag);
  response.setHeader("Cache-Control", "private, no-cache");
  response.setHeader("Cross-Origin-Resource-Policy", "same-origin");
  response.setHeader("Content-Type", asset.contentType);
  if (request.headers["if-none-match"] === asset.etag) return sendEmpty(response, 304);
  response.statusCode = 200;
  if (method === "GET") response.end(asset.content);
  else response.end();
}

function validAssetName(name: string): boolean {
  return name !== "" && !name.startsWith("/") && !name.includes("\\") && !name.split("/").some((part) => part === "" || part === "." || part === "..");
}

function sendText(response: ServerResponse, method: string, status: number, body: string): void {
  response.statusCode = status;
  response.setHeader("Content-Type", "text/plain; charset=utf-8");
  response.setHeader("Cache-Control", "no-store");
  if (method === "GET") response.end(body);
  else response.end();
}

function sendEmpty(response: ServerResponse, status: number, headers: Readonly<Record<string, string>> = {}): void {
  response.statusCode = status;
  for (const [name, value] of Object.entries(headers)) response.setHeader(name, value);
  response.end();
}
