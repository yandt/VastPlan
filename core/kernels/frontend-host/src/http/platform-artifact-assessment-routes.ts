import type { IncomingMessage, ServerResponse } from "node:http";
import type { PlatformCapabilityPort } from "../capabilities/platform-management-client";
import type { PlatformManagementTarget } from "../capabilities/platform-management-resolver";
import type { Principal } from "../identity/identity-provider";
import { sendAPIError } from "./json-response";
import { authorizePlatformOperation, sendPlatformResponse } from "./platform-response";
import { requirePlatformRole } from "./platform-route-contract";

const capability = "platform.artifacts.repository";

export class PlatformArtifactAssessmentRoutes {
  public constructor(private readonly client: PlatformCapabilityPort) {}

  public async handle(parts: readonly string[], principal: Principal, target: PlatformManagementTarget, request: IncomingMessage, response: ServerResponse, signal: AbortSignal): Promise<boolean> {
    if (parts.length !== 3 || parts[0] !== "assessment" || parts[1] !== "reports") return false;
    const method = request.method ?? "GET";
    if (method !== "GET" && method !== "HEAD") { sendAPIError(response, 405, "method_not_allowed", method === "HEAD"); return true; }
    const digest = parts[2] ?? "";
    if (!/^[a-f0-9]{64}$/.test(digest)) { sendAPIError(response, 400, "invalid_assessment_report", method === "HEAD"); return true; }
    if (!authorizePlatformOperation(this.client, target, capability, "prepareAssessmentReport", false, response) || !requirePlatformRole(principal, "platform.artifacts.assessment.report.read", response)) return true;
    await sendPlatformResponse({ client: this.client, principal, target, capability, operation: "prepareAssessmentReport", write: false, payload: { sha256: digest }, response, signal, head: method === "HEAD" });
    return true;
  }
}
