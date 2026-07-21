import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, responseItems, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";

const capability = "platform.deployment";

interface DeploymentCall {
  readonly client: PlatformCapabilityPort;
  readonly principal: Principal;
  readonly target: PlatformManagementTarget;
  readonly operation: string;
  readonly write: boolean;
  readonly payload: unknown;
  readonly response: ServerResponse;
  readonly signal: AbortSignal;
  readonly head?: boolean;
  readonly items?: boolean;
}

export function authorizeDeployment(
  client: PlatformCapabilityPort,
  target: PlatformManagementTarget,
  operation: string,
  write: boolean,
  principal: Principal,
  role: string,
  response: ServerResponse,
): boolean {
  return authorizePlatformOperation(client, target, capability, operation, write, response)
    && requirePlatformRole(principal, role, response);
}

export function callDeployment(options: DeploymentCall): Promise<void> {
  return sendPlatformResponse({
    client: options.client,
    principal: options.principal,
    target: options.target,
    capability,
    operation: options.operation,
    write: options.write,
    payload: options.payload,
    response: options.response,
    signal: options.signal,
    ...(options.head === undefined ? {} : { head: options.head }),
    ...(options.items === true ? { transform: responseItems } : {}),
  });
}

export async function listDeployment(
  client: PlatformCapabilityPort,
  operation: string,
  principal: Principal,
  target: PlatformManagementTarget,
  request: IncomingMessage,
  response: ServerResponse,
  signal: AbortSignal,
): Promise<true> {
  const method = request.method ?? "GET";
  if (!authorizeDeployment(client, target, operation, false, principal, "platform.deployment.read", response)) return true;
  if (method !== "GET" && method !== "HEAD") return rejectDeploymentRoute(response, 405, "method_not_allowed", method);
  await callDeployment({ client, principal, target, operation, write: false, payload: {}, response, signal, head: method === "HEAD", items: true });
  return true;
}

export function rejectDeploymentRoute(response: ServerResponse, status: number, code: string, method: string): true {
  sendAPIError(response, status, code, method === "HEAD");
  return true;
}
