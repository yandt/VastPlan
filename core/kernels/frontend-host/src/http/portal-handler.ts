import type { IncomingMessage, ServerResponse } from "node:http";
import { PortalAssets } from "../assets/portal-assets";
import type { IdentityProvider } from "../identity/identity-provider";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import type { InteractionPort } from "../capabilities/interaction-client";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementResolver } from "../capabilities/platform-management-resolver";
import type { PortalDeliveryStore } from "../runtime/portal-delivery-store";
import type { PortalSSRPort } from "../runtime/portal-ssr-coordinator";
import { createAPIHandler } from "./api-handler";
import { setBaseSecurityHeaders, setIndexSecurityHeaders } from "./security-headers";
import type { AccessCatalogPort } from "../access/access-catalog-port";
import { serveAccessBootstrap } from "./access-bootstrap-route";
import { serveAccessBrandAsset } from "./access-brand-asset-route";

export interface PortalHandlerOptions {
  assets: PortalAssets;
  identity?: IdentityProvider;
  access?: AccessCatalogPort;
  secureCookies?: boolean;
  composer?: PortalComposerPort;
  interaction?: InteractionPort;
  platform?: { resolver: PlatformManagementResolver; client: PlatformCapabilityPort };
  delivery?: PortalDeliveryStore;
  ssr?: PortalSSRPort;
}

export function createPortalHandler(options: PortalHandlerOptions): (request: IncomingMessage, response: ServerResponse) => void {
  const api = options.identity === undefined ? undefined : createAPIHandler({
    identity: options.identity,
    secureCookies: options.secureCookies ?? true,
    ...(options.composer === undefined ? {} : { composer: options.composer }),
    ...(options.interaction === undefined ? {} : { interaction: options.interaction }),
    ...(options.platform === undefined ? {} : { platform: options.platform }),
    ...(options.delivery === undefined ? {} : { delivery: options.delivery }),
  });
  return (request, response) => {
    setBaseSecurityHeaders(response, options.secureCookies ?? true);
    const method = request.method ?? "GET";
    const path = requestPath(request.url);
    if (path === undefined) return sendEmpty(response, 400);
    if (path === "/v1" || path.startsWith("/v1/")) {
      if (api === undefined) return sendEmpty(response, 404);
      void api(request, response, path);
      return;
    }
    if (path === "/auth/v1/bootstrap") {
      if (options.access === undefined) return sendEmpty(response, 404);
      void serveAccessBootstrap(options.access, request, response);
      return;
    }
    if (path.startsWith("/auth/v1/assets/")) {
      if (options.access === undefined) return sendEmpty(response, 404);
      void serveAccessBrandAsset(options.access, options.assets, request, response, path);
      return;
    }
    if (path === "/auth/access") {
      if (method !== "GET" && method !== "HEAD") return sendEmpty(response, 405, { Allow: "GET, HEAD" });
      void serveIndex(options.assets, undefined, request, response, method, path);
      return;
    }
    if (path === "/auth" || path.startsWith("/auth/")) {
      if (options.identity?.handle === undefined) return sendEmpty(response, 404);
      void options.identity.handle(request, response, path, options.secureCookies ?? true).then((handled) => {
        if (!handled && !response.headersSent) sendEmpty(response, 404);
      }).catch((error: unknown) => {
        process.stderr.write(`${JSON.stringify({ level: "error", message: "identity route failed", error: error instanceof Error ? error.message : String(error) })}\n`);
        if (!response.headersSent) sendEmpty(response, 502);
        else response.destroy();
      });
      return;
    }
    if (method !== "GET" && method !== "HEAD") return sendEmpty(response, 405, { Allow: "GET, HEAD" });
    if (path === "/healthz" || path === "/readyz") return sendText(response, method, 200, "ok\n");
    if (path.startsWith("/assets/")) return serveAsset(options.assets, path.slice("/assets/".length), method, request, response);
    void servePage(options, request, response, method, path);
  };
}

async function servePage(options: PortalHandlerOptions, request: IncomingMessage, response: ServerResponse, method: string, path: string): Promise<void> {
  if (options.identity?.loginRedirect !== undefined) {
    try { await options.identity.authenticate(request); }
    catch {
      response.statusCode = 302;
      response.setHeader("Location", options.identity.loginRedirect(request.url ?? path));
      response.setHeader("Cache-Control", "no-store");
      response.end();
      return;
    }
  }
  await serveIndex(options.assets, options.ssr, request, response, method, path);
}

async function serveIndex(assets: PortalAssets, ssr: PortalSSRPort | undefined, request: IncomingMessage, response: ServerResponse, method: string, path: string): Promise<void> {
	let html: string | undefined;
	if (method === "GET" && ssr !== undefined) {
		try {
			html = (await ssr.render(request, path))?.html;
			response.setHeader("X-VastPlan-SSR", html === undefined ? "bypass" : "rendered");
		} catch (error) {
			response.setHeader("X-VastPlan-SSR", "fallback");
			process.stderr.write(`${JSON.stringify({ level: "error", message: "portal ssr fallback", error: error instanceof Error ? error.message : String(error) })}\n`);
		}
	}
	const index = assets.renderIndex(html);
	setIndexSecurityHeaders(response, index.nonce);
	response.statusCode = 200;
	if (method === "GET") response.end(index.body);
	else response.end();
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
