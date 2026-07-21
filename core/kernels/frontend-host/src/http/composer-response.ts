import type { ServerResponse } from "node:http";
import type { PortalComposerPort } from "../capabilities/portal-composer-client";
import { CapabilityApplicationError } from "../capabilities/portal-composer-client";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError, sendJSON } from "./json-response";

export async function sendComposerResponse(
  composer: PortalComposerPort,
  principal: Principal,
  operation: string,
  payload: Uint8Array,
  response: ServerResponse,
  signal: AbortSignal,
  head = false,
): Promise<void> {
  try {
    const raw = await composer.call(principal, operation, payload, signal);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
    catch { return sendAPIError(response, 502, "invalid_capability_response", head); }
    sendJSON(response, 200, value, head);
  } catch (error) {
    if (error instanceof CapabilityApplicationError && error.code === "permission.denied") sendAPIError(response, 403, "forbidden", head);
    else sendAPIError(response, 400, "request_rejected", head);
  }
}
