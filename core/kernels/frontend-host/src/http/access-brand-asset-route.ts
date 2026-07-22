import type { IncomingMessage, ServerResponse } from "node:http";
import type { AccessCatalogPort } from "../access/access-catalog-port";
import type { PortalAssets } from "../assets/portal-assets";
import { parseAccessTarget } from "./access-bootstrap-route";

const route = /^\/auth\/v1\/assets\/([a-f0-9]{64})\/([a-z][a-z0-9._-]{0,127})$/;
const imageTypes = new Set(["image/svg+xml", "image/png", "image/jpeg", "image/webp"]);

export async function serveAccessBrandAsset(access: AccessCatalogPort, assets: PortalAssets, request: IncomingMessage, response: ServerResponse, path: string): Promise<void> {
  const method = request.method ?? "GET";
  if (method !== "GET" && method !== "HEAD") return empty(response, 405, { Allow: "GET, HEAD" });
  const match = route.exec(path), target = parseAccessTarget(request);
  if (match === null || target === undefined) return empty(response, 400);
  try {
    const generation = await access.resolve(target.host, target.returnTo);
    if (generation === undefined || generation.id !== match[1] || generation.profile.branding.logoAssetId !== match[2] || generation.profile.branding.logoSha256 === undefined) return empty(response, 404);
    const asset = assets.getVerified(`access/${match[2]}`, generation.profile.branding.logoSha256);
    if (asset === undefined || !imageTypes.has(asset.contentType)) return empty(response, 503);
    if (request.headers["if-none-match"] === asset.etag) { response.statusCode = 304; response.setHeader("ETag", asset.etag); response.end(); return; }
    response.statusCode = 200;
    response.setHeader("Content-Type", asset.contentType);
    response.setHeader("Content-Length", String(asset.content.byteLength));
    response.setHeader("Cache-Control", "public, max-age=31536000, immutable");
    response.setHeader("ETag", asset.etag);
    response.setHeader("Vary", "Host");
    response.setHeader("Content-Security-Policy", "default-src 'none'; sandbox");
    if (method === "GET") response.end(asset.content); else response.end();
  } catch { empty(response, 503); }
}

function empty(response: ServerResponse, status: number, headers: Readonly<Record<string,string>> = {}): void { response.statusCode = status; response.setHeader("Cache-Control", "no-store"); for (const [name,value] of Object.entries(headers)) response.setHeader(name,value); response.end(); }
