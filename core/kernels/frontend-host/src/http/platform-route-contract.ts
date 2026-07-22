import type { IncomingMessage, ServerResponse } from "node:http";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";

export function requirePlatformRole(principal: Principal, role: string, response: ServerResponse): boolean {
  if (principal.system === true || principal.roles.includes(role)) return true;
  sendAPIError(response, 403, "forbidden");
  return false;
}

export function resourceName(value: string | undefined, maximum: number): string | undefined {
  if (value === undefined) return undefined;
  try {
    const decoded = decodeURIComponent(value);
    return decoded.trim() === "" || decoded.length > maximum || decoded.includes("/") || decoded.includes("\\") || decoded.includes("\0") ? undefined : decoded;
  } catch { return undefined; }
}

export function requestHostname(request: IncomingMessage): string {
  try { return new URL(`https://${request.headers.host ?? ""}`).hostname; }
  catch { return request.headers.host ?? ""; }
}

export function optionalNonnegativeVersion(url: string | undefined): number | undefined | "invalid" {
  const raw = new URL(url ?? "/", "https://portal.invalid").searchParams.get("ifVersion");
  if (raw === null || raw === "") return undefined;
  if (!/^[0-9]+$/.test(raw)) return "invalid";
  const value = Number(raw);
  return Number.isSafeInteger(value) && value >= 0 ? value : "invalid";
}

export function queryValue(url: string | undefined, name: string): string {
  return new URL(url ?? "/", "https://portal.invalid").searchParams.get(name) ?? "";
}
