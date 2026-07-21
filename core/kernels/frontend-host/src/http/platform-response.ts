import type { ServerResponse } from "node:http";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import { ManagementAuthorizationError, type PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError, sendJSON } from "./json-response";
import { encodeCapabilityPayload } from "./revision-route-contract";

export async function sendPlatformResponse(options: {
  client: PlatformCapabilityPort; principal: Principal; target: PlatformManagementTarget;
  capability: string; operation: string; write: boolean; payload: unknown;
  response: ServerResponse; signal: AbortSignal; head?: boolean; transform?: (value: unknown) => unknown;
}): Promise<void> {
  try {
    const raw = await options.client.call(options.principal, options.target, options.capability, options.operation, options.write, encodeCapabilityPayload(options.payload), options.signal);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
    catch { return sendAPIError(options.response, 502, "platform_service_unavailable", options.head); }
    sendJSON(options.response, 200, options.transform?.(value) ?? value, options.head);
  } catch (error) {
    if (error instanceof ManagementAuthorizationError) return sendAPIError(options.response, 403, "management_binding_forbidden", options.head);
    if (error instanceof CapabilityApplicationError) return mapCapabilityError(options.response, error.code, options.head);
    sendAPIError(options.response, 502, "platform_service_unavailable", options.head);
  }
}

export function authorizePlatformOperation(client: PlatformCapabilityPort, target: PlatformManagementTarget, capability: string, operation: string, write: boolean, response: ServerResponse): boolean {
  try { client.authorize(target, capability, operation, write); return true; }
  catch { sendAPIError(response, 403, "management_binding_forbidden"); return false; }
}

export function responseItems(value: unknown): unknown[] {
  if (typeof value !== "object" || value === null || !Array.isArray((value as Record<string, unknown>).items)) throw new Error("平台列表响应无效");
  return (value as { items: unknown[] }).items;
}

function mapCapabilityError(response: ServerResponse, code: string, head = false): void {
  if (code === "permission.denied") return sendAPIError(response, 403, "forbidden", head);
  if (["platform.settings.not_found", "platform.credentials.not_found", "platform.database.not_found", "platform.deployment.not_found"].includes(code)) return sendAPIError(response, 404, "not_found", head);
  if (["platform.settings.version_conflict", "platform.deployment.version_conflict"].includes(code)) return sendAPIError(response, 409, "version_conflict", head);
  if (code === "platform.deployment.separation_required") return sendAPIError(response, 409, "separation_required", head);
  if (code === "platform.deployment.job_conflict") return sendAPIError(response, 409, "job_conflict", head);
  if (code === "platform.deployment.service_state_conflict") return sendAPIError(response, 409, "service_state_conflict", head);
  if (["platform.settings.invalid", "platform.credentials.invalid", "platform.database.invalid", "platform.deployment.invalid"].includes(code)) return sendAPIError(response, 400, "invalid_request", head);
  if (code === "platform.deployment.bootstrap_failed") return sendAPIError(response, 502, "bootstrap_failed", head);
  if (code === "platform.deployment.service_publish_failed") return sendAPIError(response, 502, "service_publish_failed", head);
  sendAPIError(response, 502, "platform_service_unavailable", head);
}
