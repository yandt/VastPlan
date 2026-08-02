import type { ServerResponse } from "node:http";
import { CapabilityApplicationError } from "../capabilities/capability-invoker";
import type { Principal } from "../identity/identity-provider";
import { reportCapabilityFailure } from "../observability/capability-failure";
import { sendAPIError, sendJSON } from "./json-response";

export interface JSONCapabilityPort {
  call(principal: Principal, operation: string, payload: Uint8Array, signal?: AbortSignal): Promise<Uint8Array>;
}

export async function sendCapabilityResponse(
  capability: JSONCapabilityPort,
  principal: Principal,
  operation: string,
  payload: Uint8Array,
  response: ServerResponse,
  signal: AbortSignal,
  head = false,
): Promise<void> {
  try {
    const raw = await capability.call(principal, operation, payload, signal);
    let value: unknown;
    try { value = JSON.parse(new TextDecoder().decode(raw)) as unknown; }
    catch { return sendAPIError(response, 502, "invalid_capability_response", head); }
    sendJSON(response, 200, value, head);
  } catch (error) {
    reportCapabilityFailure({ operation }, error);
	if (error instanceof CapabilityApplicationError && error.code === "permission.denied") sendAPIError(response, 403, "forbidden", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.separation_required") sendAPIError(response, 409, "approval_separation_required", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.review_required") sendAPIError(response, 409, "approval_review_required", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.evidence.mismatch") sendAPIError(response, 409, "approval_evidence_mismatch", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.provider_unavailable") sendAPIError(response, 503, "approval_provider_unavailable", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.digest_mismatch") sendAPIError(response, 409, "approval_digest_mismatch", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.approval.reason_required") sendAPIError(response, 400, "approval_reason_required", head);
    else if (error instanceof CapabilityApplicationError && error.code === "portal.catalog.rejected") sendAPIError(response, 409, "portal_catalog_rejected", head);
    else sendAPIError(response, 400, "request_rejected", head);
  }
}
