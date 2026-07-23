import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";

const capability = "platform.artifacts.repository";
const simpleQueries: Readonly<Record<string, string>> = Object.freeze({ status: "status", capacity: "capacity", references: "listReferences", migration: "migrationStatus" });
const gcQueries: Readonly<Record<string, string>> = Object.freeze({ plan: "gcPlan", status: "gcStatus" });
const assessmentQueries: Readonly<Record<string, string>> = Object.freeze({ inventory: "assessmentInventory" });

export class PlatformArtifactQueryRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    let operation = parts.length === 1 ? simpleQueries[parts[0]!] : parts.length === 2 && parts[0] === "gc" ? gcQueries[parts[1]!] : parts.length === 2 && parts[0] === "assessment" ? assessmentQueries[parts[1]!] : undefined;
    const catalog = parts.length === 1 && parts[0] === "catalog";
    if (catalog) operation = "listCatalog";
    if (operation === undefined) return false;
    if (!authorizePlatformOperation(this.client, target, capability, operation, false, response) || !requirePlatformRole(principal, "platform.artifacts.read", response)) return true;
    const method = request.method ?? "GET";
    if (method !== "GET" && method !== "HEAD") { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
    const payload = catalog ? catalogQuery(request.url) : {};
    if (payload === undefined) { sendAPIError(response, 400, "invalid_catalog_query"); return true; }
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation, write: false, payload, response, signal, head: method === "HEAD" });
    return true;
  }
}

function catalogQuery(url: string | undefined): Record<string, unknown> | undefined {
  const allowed = new Set(["pluginId", "pluginPrefix", "namespace", "publisher", "version", "channel", "target", "lifecycle", "page", "pageSize"]);
  const params = new URL(url ?? "/", "https://portal.invalid").searchParams;
  const values = new Map<string, string>();
  for (const [key, value] of params) {
    if (!allowed.has(key) || values.has(key)) return undefined;
    values.set(key, value);
  }
  const page = boundedInteger(values.get("page"), 1, 1_000_000);
  const pageSize = boundedInteger(values.get("pageSize"), 20, 100);
  if (page === undefined || pageSize === undefined) return undefined;
  for (const key of ["pluginId", "pluginPrefix", "namespace", "publisher", "version", "channel"]) {
    const value = values.get(key);
    if (value !== undefined && (value.length > 160 || /[\u0000\r\n]/.test(value))) return undefined;
  }
  const target = values.get("target");
  if (target !== undefined && target !== "" && !["backend", "frontend", "runner", "mobile"].includes(target)) return undefined;
  const lifecycle = values.get("lifecycle");
  if (lifecycle !== undefined && lifecycle !== "" && !["active", "deprecated", "yanked", "revoked"].includes(lifecycle)) return undefined;
  return Object.fromEntries([...values].filter(([key, value]) => key !== "page" && key !== "pageSize" && value !== "").concat([["page", String(page)], ["pageSize", String(pageSize)]]).map(([key, value]) => [key, key === "page" || key === "pageSize" ? Number(value) : value]));
}

function boundedInteger(value: string | undefined, fallback: number, maximum: number): number | undefined {
  if (value === undefined || value === "") return fallback;
  if (!/^[0-9]+$/.test(value)) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed >= 1 && parsed <= maximum ? parsed : undefined;
}
