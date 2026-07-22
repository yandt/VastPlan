import type { IncomingMessage, ServerResponse } from "node:http";
import type { AccessCatalogPort } from "../access/access-catalog-port";

export async function serveAccessBootstrap(
  access: AccessCatalogPort,
  request: IncomingMessage,
  response: ServerResponse,
): Promise<void> {
  const method = request.method ?? "GET";
  if (method !== "GET" && method !== "HEAD") return sendEmpty(response, 405, { Allow: "GET, HEAD" });
  const target = parseAccessTarget(request);
  if (target === undefined) return sendEmpty(response, 400);
  try {
    const generation = await access.resolve(target.host, target.returnTo);
    if (generation === undefined) return sendEmpty(response, 404);
    const body = Buffer.from(JSON.stringify(generation.bootstrap));
    response.statusCode = 200;
    response.setHeader("Content-Type", "application/json; charset=utf-8");
    response.setHeader("Cache-Control", "no-store");
    response.setHeader("Vary", "Host");
    response.setHeader("Content-Length", String(body.byteLength));
    if (method === "GET") response.end(body);
    else response.end();
  } catch (error) {
    process.stderr.write(`${JSON.stringify({ level: "error", message: "access bootstrap failed", error: error instanceof Error ? error.message : String(error) })}\n`);
    if (!response.headersSent) sendEmpty(response, 503);
    else response.destroy();
  }
}

export function parseAccessTarget(request: IncomingMessage): { host: string; returnTo: string } | undefined {
  const rawHost = request.headers.host;
  if (rawHost === undefined || rawHost.includes(",") || rawHost.length > 512) return undefined;
  let host: string;
  try {
    const origin = new URL(`https://${rawHost}`);
    if (origin.username !== "" || origin.password !== "" || origin.pathname !== "/" || origin.search !== "" || origin.hash !== "") return undefined;
    host = origin.hostname.toLowerCase().replace(/\.$/, "");
  } catch { return undefined; }
  let url: URL;
  try { url = new URL(request.url ?? "/auth/v1/bootstrap", "https://portal.invalid"); }
  catch { return undefined; }
  const returnTo = url.searchParams.get("returnTo") ?? "/";
  if (!isSafeReturnTo(returnTo)) return undefined;
  return { host, returnTo: new URL(returnTo, "https://portal.invalid").pathname };
}

function isSafeReturnTo(value: string): boolean {
  return value.length <= 2048 && value.startsWith("/") && !value.startsWith("//") && !value.includes("\\") && !/[\0\r\n]/.test(value);
}

function sendEmpty(response: ServerResponse, status: number, headers: Readonly<Record<string, string>> = {}): void {
  response.statusCode = status;
  response.setHeader("Cache-Control", "no-store");
  for (const [name, value] of Object.entries(headers)) response.setHeader(name, value);
  response.end();
}
